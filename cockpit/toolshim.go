package cockpit

// shim.go is the dispatcher of a tool shim (ADR-014): a call from a calling environment that is
// answered from a record of the same call, or run in the tool's own pinned image and recorded.
//
// Nothing the shim sends is trusted (ADR-014, section 8). The dispatcher takes the files' bytes,
// never their hashes, re-checks every path before it stages anything, runs the tool on exactly what
// it staged, and records only what that run left as regular files. So the record describes the run
// the dispatcher made, whatever the caller claimed.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/kton-protocol/kton-cockpit/cockpit/executor"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/toolcall"
	"kton.dev/plankton/core"
)

// ToolCall is one call of a tool, as a shim hands it over.
type ToolCall struct {
	// Tool is the name the shim is installed under (nmfe75); it must be configured under shims.
	Tool string `json:"tool"`
	// Args are the arguments as the key sees them (toolcall.NormArgv), without the tool's name.
	Args []string `json:"args"`
	// Cwd is the call's directory relative to the repository root; empty is the root.
	Cwd string `json:"cwd"`
	// Collector is how the shim found Files. It must be the one the configuration names.
	Collector string `json:"collector"`
	// Files are the call's inputs, relative to the repository root, with their bytes.
	Files []CallFile `json:"files"`
}

// CallFile is one file of a call, relative to the repository root.
type CallFile struct {
	Path string `json:"path"`
	Data []byte `json:"data"`
}

// ToolResult is what the call did, for the shim to put in place.
type ToolResult struct {
	// Files are the outputs, relative to the repository root.
	Files    []CallFile `json:"files"`
	Stdout   []byte     `json:"stdout"`
	Stderr   []byte     `json:"stderr"`
	ExitCode int        `json:"exitCode"`
	// Reused says the answer came from the record FotonID, and nothing ran. Otherwise the tool ran,
	// and FotonID is set when that run was recorded.
	Reused  bool   `json:"reused"`
	FotonID string `json:"fotonId,omitempty"`
	// Note says in one line what happened, for the shim to print on stderr.
	Note string `json:"note"`
}

// The files a run's own output streams and exit code are kept in, beside its outputs: PsN and
// pharmpy parse them, so they are outputs too.
const (
	streamsDir = ".kton"
	stdoutFile = streamsDir + "/stdout"
	stderrFile = streamsDir + "/stderr"
	exitFile   = streamsDir + "/exit-code"
)

// Call answers one tool call: from this repository's own record of the same call when there is
// one, or by running the tool in its configured image on exactly the staged inputs and recording
// that run.
func (c *Cockpit) Call(ctx context.Context, in ToolCall) (*ToolResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	sh, ok := cfg.Raw.Shims[in.Tool]
	if !ok {
		return nil, refuse("shim.unknown", "ADR-014", "%q is not a tool this repository's shims configure", in.Tool)
	}
	if in.Collector != sh.Collector {
		return nil, refuse("shim.collector", "ADR-014", "the shim collected with %q, but %s is configured with %q",
			in.Collector, in.Tool, sh.Collector)
	}
	if err := checkCall(in); err != nil {
		return nil, refuse("shim.path", "ADR-014, section 8", "%v", err)
	}
	rebased, base := rebase(in)
	res, err := c.call(ctx, cfg, sh, rebased)
	if res != nil && base != "" {
		for i := range res.Files {
			res.Files[i].Path = path.Join(base, res.Files[i].Path)
		}
	}
	return res, err
}

// rebase moves a call to its base, the deepest directory that holds its directory and all its
// inputs, and returns the base. A call is keyed, staged and recorded relative to it: where in the
// repository it ran says nothing about what it did, and a caller that runs each call in a fresh
// directory (pharmpy's NONMEM_run_<model>-<uuid>) still meets its own records. On the way in,
// The outputs go back under the base.
func rebase(in ToolCall) (ToolCall, string) {
	segs := func(p string) []string {
		if p == "" || p == "." {
			return nil
		}
		return strings.Split(p, "/")
	}
	base := segs(in.Cwd)
	// An argument may reach above the call's directory (an output ../out.csv, a listing in ../lst/);
	// the base holds whatever it reaches.
	up := 0
	for _, a := range in.Args {
		if _, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "-") {
			a = v
		}
		n := 0
		for strings.HasPrefix(a, "../") || a == ".." {
			n, a = n+1, strings.TrimPrefix(strings.TrimPrefix(a, ".."), "/")
		}
		up = max(up, n)
	}
	base = base[:max(0, len(base)-up)]
	for _, f := range in.Files {
		dir := segs(path.Dir(f.Path))
		n := 0
		for n < len(base) && n < len(dir) && base[n] == dir[n] {
			n++
		}
		base = base[:n]
	}
	out := in
	prefix := strings.Join(base, "/")
	if prefix == "" {
		return out, ""
	}
	out.Cwd = strings.TrimPrefix(strings.TrimPrefix(out.Cwd, prefix), "/")
	out.Files = make([]CallFile, len(in.Files))
	for i, f := range in.Files {
		out.Files[i] = CallFile{Path: strings.TrimPrefix(f.Path, prefix+"/"), Data: f.Data}
	}
	return out, prefix
}

func (c *Cockpit) call(ctx context.Context, cfg *config.Config, sh config.Shim, in ToolCall) (*ToolResult, error) {

	stage, err := os.MkdirTemp("", "cockpit-shim-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	inputs := make([]string, 0, len(in.Files))
	staged := map[string]string{} // path → hash
	for _, f := range in.Files {
		if err := stageFile(stage, f); err != nil {
			return nil, err
		}
		inputs = append(inputs, f.Path)
		staged[f.Path] = hashOf(f.Data)
	}
	args := make([]any, len(in.Args))
	for i, a := range in.Args {
		args[i] = a
	}
	r := binaries.New(cfg)
	rec := binaries.AuthorInput{
		Dir: stage, Inputs: inputs, SignKey: cfg.PlanktonKey, EnvRef: sh.Image,
		Kind:       "tool/" + in.Tool,
		Descriptor: map[string]any{"tool": in.Tool, "args": args, "cwd": in.Cwd, "collector": sh.Collector},
	}
	key, err := r.ActionKey(rec)
	if err != nil {
		return nil, err
	}
	cas := &toolcall.CAS{Dir: casDir()}

	hits, err := r.OwnHits(key, cfg.PlanktonKey)
	if err != nil {
		return nil, err
	}
	for _, h := range hits {
		if res, ok := fromCAS(cas, h); ok {
			res.Note = fmt.Sprintf("kton: %s reused %s", in.Tool, h.ID)
			return res, nil
		}
	}

	res, outputs, recordable, why, err := runStaged(ctx, stage, in, sh, staged)
	if err != nil {
		return nil, err
	}
	if !recordable {
		res.Note = fmt.Sprintf("kton: %s ran in %s and was not recorded: %s", in.Tool, sh.Image, why)
		return res, nil
	}
	for _, o := range outputs {
		if _, err := cas.Put(filepath.Join(stage, o), staged[o]); err != nil {
			return nil, fmt.Errorf("keeping %s: %w", o, err)
		}
	}
	rec.Outputs = outputs
	a, err := r.Author(ctx, rec)
	if err != nil {
		return nil, fmt.Errorf("recording the run: %w", err)
	}
	res.FotonID = a.ID
	res.Note = fmt.Sprintf("kton: %s ran in %s and was recorded as %s", in.Tool, sh.Image, a.ID)
	return res, nil
}

// checkCall refuses a path that could reach outside the staging directory, or that names the files
// the run's own streams are kept in.
func checkCall(in ToolCall) error {
	if in.Cwd != "" {
		if err := toolcall.CheckRel(in.Cwd); err != nil {
			return fmt.Errorf("cwd: %w", err)
		}
	}
	seen := map[string]bool{}
	for _, f := range in.Files {
		if err := toolcall.CheckRel(f.Path); err != nil {
			return err
		}
		if f.Path == streamsDir || strings.HasPrefix(f.Path, streamsDir+"/") {
			return fmt.Errorf("%q: %s is where the run's own streams are kept", f.Path, streamsDir)
		}
		if seen[f.Path] {
			return fmt.Errorf("%q is sent twice", f.Path)
		}
		seen[f.Path] = true
	}
	for _, a := range in.Args {
		if strings.ContainsRune(a, 0) {
			return fmt.Errorf("an argument contains a NUL byte")
		}
	}
	return nil
}

// stageFile writes one input under root. The directory is fresh and only this function creates
// anything in it, but each file is still created exclusively and without following a link.
func stageFile(root string, f CallFile) error {
	dst := filepath.Join(root, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	w, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return fmt.Errorf("staging %s: %w", f.Path, err)
	}
	if _, err := w.Write(f.Data); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// runStaged runs the call in its image on the staged directory and reads back what it left. It
// returns the outputs (relative paths, their hashes added to staged) and whether the run may be
// recorded, with the reason when not. A link, device or pipe in the directory is an error: such a
// run's outputs are not read at all (ADR-014, section 8, D2).
func runStaged(ctx context.Context, stage string, in ToolCall, sh config.Shim, staged map[string]string) (*ToolResult, []string, bool, string, error) {
	if err := os.Mkdir(filepath.Join(stage, streamsDir), 0o755); err != nil {
		return nil, nil, false, "", err
	}
	dir := in.Cwd
	if dir == "" {
		dir = "."
	}
	// The call's directory exists in the repository it came from, even when no input lies in it.
	if err := os.MkdirAll(filepath.Join(stage, filepath.FromSlash(dir)), 0o755); err != nil {
		return nil, nil, false, "", err
	}
	cmd := fmt.Sprintf("(cd %s && exec %s) >%s 2>%s; echo $? >%s",
		toolcall.ShellJoin([]string{dir}), toolcall.ShellJoin(append([]string{in.Tool}, in.Args...)),
		stdoutFile, stderrFile, exitFile)
	ex, ok := executor.Lookup("")
	if !ok {
		return nil, nil, false, "", fmt.Errorf("no container executor in this build")
	}
	var mounts []executor.Mount
	var secrets [][]byte
	for _, s := range sh.Secrets {
		host := expandHome(s.Host)
		b, err := os.ReadFile(host)
		if err != nil {
			return nil, nil, false, "", fmt.Errorf("shims.%s.secrets: %w", in.Tool, err)
		}
		secrets = append(secrets, b)
		mounts = append(mounts, executor.Mount{Host: host, At: s.At})
	}
	if _, err := ex.Run(ctx, executor.Step{Root: stage, Image: sh.Image, Engine: sh.Engine, ReadOnly: mounts, NoEntrypoint: true, Cmd: cmd}); err != nil {
		return nil, nil, false, "", err
	}

	var outputs []string
	files := map[string][]byte{}
	limit, total := sh.MaxOutputBytes, int64(0)
	if limit == 0 {
		limit = 1 << 30
	}
	err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(stage, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			return fmt.Errorf("the run left %s, which is not a regular file (%s); nothing of it is read or recorded", rel, d.Type())
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if total += fi.Size(); total > limit {
			return fmt.Errorf("the run left more than %d bytes (shims.%s.maxOutputBytes); nothing of it is read or recorded", limit, in.Tool)
		}
		b, err := readNoFollow(p)
		if err != nil {
			return err
		}
		h := hashOf(b)
		if old, ok := staged[rel]; ok && old == h {
			return nil // an input the run left as it was
		}
		staged[rel] = h
		files[rel] = b
		outputs = append(outputs, rel)
		return nil
	})
	if err != nil {
		return nil, nil, false, "", err
	}
	slices.Sort(outputs)

	res := &ToolResult{Stdout: files[stdoutFile], Stderr: files[stderrFile], ExitCode: -1}
	if code, err := strconv.Atoi(strings.TrimSpace(string(files[exitFile]))); err == nil {
		res.ExitCode = code
	}
	for _, o := range outputs {
		if !strings.HasPrefix(o, streamsDir+"/") {
			res.Files = append(res.Files, CallFile{Path: o, Data: files[o]})
		}
	}

	switch {
	case res.ExitCode != 0 && !slices.Contains(sh.ResultCodes, res.ExitCode):
		return res, outputs, false, fmt.Sprintf("exit code %d is not a result", res.ExitCode), nil
	}
	for _, e := range sh.Expect {
		want := path.Join(in.Cwd, expandArgs(e, in.Args))
		if _, ok := files[want]; !ok {
			return res, outputs, false, fmt.Sprintf("it did not write %s, which a result always has", want), nil
		}
	}
	for _, sec := range secrets {
		for _, o := range outputs {
			if len(sec) > 0 && bytes.Contains(files[o], sec) {
				return res, outputs, false, fmt.Sprintf("%s contains a configured secret", o), nil
			}
		}
	}
	return res, outputs, true, "", nil
}

// fromCAS answers a call from a record whose every output the content store keeps, checked
// against its hash.
func fromCAS(cas *toolcall.CAS, h binaries.Reused) (*ToolResult, bool) {
	res := &ToolResult{Reused: true, FotonID: h.ID, ExitCode: -1}
	for _, o := range h.Outputs {
		b, err := readNoFollow(cas.Path(o.Hash))
		if err != nil || hashOf(b) != o.Hash {
			return nil, false
		}
		switch o.Path {
		case stdoutFile:
			res.Stdout = b
		case stderrFile:
			res.Stderr = b
		case exitFile:
			if code, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				res.ExitCode = code
			}
		default:
			if strings.HasPrefix(o.Path, streamsDir+"/") {
				continue // the run's own, as on a miss
			}
			res.Files = append(res.Files, CallFile{Path: o.Path, Data: b})
		}
	}
	return res, true
}

func readNoFollow(p string) ([]byte, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func hashOf(b []byte) string { return core.HashBytes(b) }

// expandArgs replaces $1 … $9 with the call's arguments.
func expandArgs(s string, args []string) string {
	for i := 9; i >= 1; i-- {
		v := ""
		if i <= len(args) {
			v = args[i-1]
		}
		s = strings.ReplaceAll(s, "$"+strconv.Itoa(i), v)
	}
	return s
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// casDir is where a shim's outputs are kept: the content store kton-call uses too.
func casDir() string {
	if d := os.Getenv("KTON_CAS"); d != "" {
		return d
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	return filepath.Join(cache, "kton-cockpit", "cas")
}
