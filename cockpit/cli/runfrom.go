package cli

// runfrom.go is a run folder started from somebody's record: `cockpit run new <slug> --from
// sha256:<foton>`. It is the lookup uat/e2e.sh did by hand — read the foton, follow each input's
// commit-pinned locator, check the bytes against the hash the record names — so that re-running
// another party's work starts from exactly what they started from, or does not start.
//
// After `cockpit run <slug>`, reproductionReport compares the outputs with the record's and, when
// the bytes agree, prints the `say` that claims it. It does not say it: a claim is a verb.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"kton.dev/plankton/core"
)

const reproducesLine = "Reproduces: "

func runNewFromFoton(ctx context.Context, slug, id string) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	rec, err := binaries.New(cfg).FotonByID(ctx, id)
	if err != nil {
		return fmt.Errorf("%v — a peer's records are read through federation.sources", err)
	}
	if rec.SignerKeyID == "" {
		return fmt.Errorf("%s is signed by no key in this repository's trust tiers — take the peer's keys in first: cockpit trust add <tier> <peer>", id)
	}
	dst := filepath.Join(cfg.RepoRoot, runsDir, slug)
	if _, serr := os.Stat(dst); serr == nil {
		return fmt.Errorf("%s/%s already exists", runsDir, slug)
	}
	made := false
	defer func() {
		if !made {
			os.RemoveAll(dst)
		}
	}()

	// A record made by `cockpit run` is a run folder already: `cd <dir> && <entry>` over files
	// under <dir>. It is laid out again as one, so the clone runs the same command.
	files, script, body := layoutOf(rec)

	f := newFetcher()
	defer f.close()
	for _, in := range files {
		b, from, err := f.fetch(ctx, in.FotonFile)
		if err != nil {
			return fmt.Errorf("input %s: %w", in.Path, err)
		}
		to := filepath.Join(dst, filepath.FromSlash(in.to))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(to, b, 0o644); err != nil {
			return err
		}
		fmt.Printf("  %-40s %s  (%s)\n", in.to, in.Hash[:19]+"…", from)
	}
	if body != "" {
		if err := os.WriteFile(filepath.Join(dst, script), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(dst, "out"), 0o755); err != nil {
		return err
	}
	note := fmt.Sprintf("# %s\n\n%s%s\nSigned by: keyid %s\nRecorded in: %s\nCommand: %s\n\n## What I am trying\n\nTo reproduce it.\n",
		slug, reproducesLine, id, rec.SignerKeyID, orNone(rec.EnvRef), rec.Cmd)
	if err := os.WriteFile(filepath.Join(dst, "RUN.md"), []byte(note), 0o644); err != nil {
		return err
	}
	made = true

	fmt.Printf("\n%s/%s — every input matches the hash %s records\n", runsDir, slug, short16(id))
	fmt.Printf("  %-11s %s\n", script, rec.Cmd)
	if rec.EnvRef != "" && rec.EnvRef != cfg.Raw.Execution.Image {
		fmt.Printf("\n  it ran in   %s\n  you pin     %s\n", rec.EnvRef, orNone(cfg.Raw.Execution.Image))
		fmt.Printf("  to run where it ran:  cockpit pin %s\n", strings.TrimPrefix(rec.EnvRef, "oci://"))
	}
	fmt.Printf("\n  then:  cockpit run %s\n", slug)
	return nil
}

type placed struct {
	binaries.FotonFile
	to string // where it goes inside the run folder
}

var cdPrefix = regexp.MustCompile(`^cd (\S+) && (.+)$`)

// layoutOf decides where a record's inputs go in a run folder and what runs them. A run-folder
// record keeps its shape; any other record keeps its paths under inputs/ and gets a run.sh that runs
// its command there and moves its outputs into out/ — moved, so a second run does not take them for
// inputs.
func layoutOf(rec *binaries.FotonDetail) (files []placed, script, body string) {
	if m := cdPrefix.FindStringSubmatch(rec.Cmd); m != nil {
		dir, rest := m[1]+"/", m[2]
		ok := true
		for _, in := range rec.Inputs {
			ok = ok && strings.HasPrefix(in.Path, dir)
		}
		for _, e := range entrypoints {
			if ok && rest == e.cmd(e.name) {
				for _, in := range rec.Inputs {
					files = append(files, placed{in, strings.TrimPrefix(in.Path, dir)})
				}
				return files, e.name, ""
			}
		}
	}
	for _, in := range rec.Inputs {
		files = append(files, placed{in, path.Join("inputs", in.Path)})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# The command %s recorded, run where its inputs are; its outputs then go to out/.\n", short16(rec.ID))
	fmt.Fprintf(&b, "set -e\ncd inputs\n%s\n", rec.Cmd)
	for _, o := range rec.Outputs {
		fmt.Fprintf(&b, "mv %q ../out/\n", o.Path)
	}
	return files, "run.sh", b.String()
}

// fetcher gets a file's bytes by its locators and accepts them only when they hash to what the
// record names. A raw.githubusercontent.com locator that cannot be fetched over HTTP is tried as
// git at the same commit, which also works offline against a local mirror (url.<x>.insteadOf).
type fetcher struct {
	clones map[string]string
	client *http.Client
}

func newFetcher() *fetcher {
	return &fetcher{clones: map[string]string{}, client: &http.Client{Timeout: 30 * time.Second}}
}

func (f *fetcher) close() {
	for _, d := range f.clones {
		os.RemoveAll(d)
	}
}

var rawGitHub = regexp.MustCompile(`^https://raw\.githubusercontent\.com/([^/]+)/([^/]+)/([0-9a-f]{40})/(.+)$`)

func (f *fetcher) fetch(ctx context.Context, file binaries.FotonFile) ([]byte, string, error) {
	if len(file.URI) == 0 {
		return nil, "", fmt.Errorf("the record names no locator, so its bytes cannot be fetched")
	}
	var tried []string
	for _, u := range file.URI {
		if b, err := f.http(ctx, u); err == nil {
			if core.HashBytes(b) == file.Hash {
				return b, u, nil
			}
			tried = append(tried, u+": the bytes do not match the recorded hash")
		} else {
			tried = append(tried, u+": "+err.Error())
		}
		if m := rawGitHub.FindStringSubmatch(u); m != nil {
			repo := "https://github.com/" + m[1] + "/" + m[2] + ".git"
			b, err := f.git(ctx, repo, m[3], m[4])
			if err == nil && core.HashBytes(b) == file.Hash {
				return b, "git " + m[1] + "/" + m[2] + "@" + m[3][:12], nil
			}
			if err == nil {
				err = fmt.Errorf("the bytes do not match the recorded hash")
			}
			tried = append(tried, repo+" @"+m[3][:12]+": "+err.Error())
		}
	}
	return nil, "", fmt.Errorf("no locator yields the recorded bytes:\n    %s", strings.Join(tried, "\n    "))
}

func (f *fetcher) http(ctx context.Context, u string) ([]byte, error) {
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return nil, fmt.Errorf("not an http locator")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<30))
}

func (f *fetcher) git(ctx context.Context, repo, sha, p string) ([]byte, error) {
	dir, ok := f.clones[repo]
	if !ok {
		tmp, err := os.MkdirTemp("", "cockpit-fetch-")
		if err != nil {
			return nil, err
		}
		if out, err := exec.CommandContext(ctx, "git", "clone", "--quiet", "--bare", repo, tmp).CombinedOutput(); err != nil {
			os.RemoveAll(tmp)
			return nil, fmt.Errorf("git clone: %s", strings.TrimSpace(string(out)))
		}
		f.clones[repo], dir = tmp, tmp
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "show", sha+":"+p).Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s: %w", sha[:12], p, err)
	}
	return out, nil
}

// reproductionReport runs after `cockpit run <slug>` of a folder made from a record: same bytes,
// and the say that claims it; different bytes, and which.
func reproductionReport(ctx context.Context, cfg *config.Config, rel string, out *cockpit.PublishResult) {
	theirID := reproducesOf(filepath.Join(cfg.RepoRoot, filepath.FromSlash(rel), "RUN.md"))
	if theirID == "" {
		return
	}
	rec, err := binaries.New(cfg).FotonByID(ctx, theirID)
	if err != nil {
		fmt.Printf("\ncannot compare with %s: %v\n", short16(theirID), err)
		return
	}
	mine := map[string]string{} // hash -> my path
	for p, h := range out.OutputHashes {
		mine[h] = p
	}
	var same, differ []string
	for _, o := range rec.Outputs {
		if p, ok := mine[o.Hash]; ok {
			same = append(same, p)
		} else {
			differ = append(differ, o.Path)
		}
	}
	sort.Strings(differ)
	if len(differ) == 0 && len(same) > 0 {
		fmt.Printf("\nsame bytes as %s — every output it records. To claim it (the cockpit measures the level):\n", short16(theirID))
		fmt.Printf("  cockpit say reproduces %s\n", rel)
		return
	}
	fmt.Printf("\nnot the same bytes as %s: %d of its %d output(s) differ (%s)\n",
		short16(theirID), len(differ), len(rec.Outputs), strings.Join(differ, ", "))
}

func reproducesOf(runMD string) string {
	fh, err := os.Open(runMD)
	if err != nil {
		return ""
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), reproducesLine); ok && strings.HasPrefix(v, "sha256:") {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func short16(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 16 {
		id = id[:16]
	}
	return "sha256:" + id + "…"
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
