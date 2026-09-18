package main

// run.go is the run folder: the unit of experimenting.
//
// A run is a directory holding what you started from, what you changed, and what came out. The
// point of making it a first-class thing is that it removes two chores that were never the user's
// to do. You do not name the outputs, because when the cockpit executes the command it is the only
// party that can see what the run produced. And you do not copy the inputs in or clear the last
// attempt out, because that is bookkeeping, and bookkeeping done by hand is bookkeeping done wrong
// on the twentieth run.
//
//	cockpit run new <slug> --from <dir>   a run folder, inputs copied in, out/ empty
//	cockpit run <slug>                    execute it in the pinned image, record what came out
//	cockpit run list                      the run folders, and which of them are recorded

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/tools"
)

// runsDir is where run folders live, relative to the repo root.
const runsDir = "runs"

// entrypoints maps the file a run folder is expected to contain to how it is invoked. A convention
// rather than a declaration: a run folder with one obvious script in it should not also need a file
// saying which script it is.
var entrypoints = []struct {
	name string
	cmd  func(path string) string
}{
	{"analysis.R", func(p string) string { return "Rscript " + p }},
	{"analysis.py", func(p string) string { return "python3 " + p }},
	{"analysis.sh", func(p string) string { return "sh " + p }},
	{"run.R", func(p string) string { return "Rscript " + p }},
	{"run.py", func(p string) string { return "python3 " + p }},
	{"run.sh", func(p string) string { return "sh " + p }},
}

func runRun(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage:\n  cockpit run new <slug> --from <dir>\n  cockpit run <slug>\n  cockpit run list")
	}
	switch args[0] {
	case "new":
		return runNew(ctx, args[1:])
	case "list":
		return runList(ctx)
	default:
		return runExecute(ctx, args[0])
	}
}

func runNew(ctx context.Context, args []string) error {
	var slug, from string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from":
			if i+1 >= len(args) {
				return fmt.Errorf("--from needs a directory")
			}
			from = args[i+1]
			i++
		default:
			if slug != "" {
				return fmt.Errorf("unexpected argument %q", args[i])
			}
			slug = args[i]
		}
	}
	if slug == "" || from == "" {
		return fmt.Errorf("usage: cockpit run new <slug> --from <dir>\n" +
			"  <dir> is an example, or another run folder — yours or a teammate's")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	src := filepath.Join(cfg.RepoRoot, filepath.FromSlash(from))
	if fi, serr := os.Stat(src); serr != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", from)
	}
	dst := filepath.Join(cfg.RepoRoot, runsDir, slug)
	if _, serr := os.Stat(dst); serr == nil {
		return fmt.Errorf("%s/%s already exists", runsDir, slug)
	}

	// What travels: the inputs and the script. What does not: the previous run's outputs, and its
	// record of having been published. Cloning somebody's run means starting where they started,
	// not arriving where they arrived.
	if err := os.MkdirAll(filepath.Join(dst, "out"), 0o755); err != nil {
		return err
	}
	copied := 0
	if _, serr := os.Stat(filepath.Join(src, "inputs")); serr == nil {
		n, cerr := copyTree(filepath.Join(src, "inputs"), filepath.Join(dst, "inputs"))
		if cerr != nil {
			return cerr
		}
		copied = n
	} else {
		// An example directory keeps its inputs beside the script rather than in inputs/.
		n, cerr := copyDeclaredInputs(cfg.RepoRoot, src, filepath.Join(dst, "inputs"))
		if cerr != nil {
			return cerr
		}
		copied = n
	}
	if copied == 0 {
		return fmt.Errorf("%s has no inputs to clone — expected %s/inputs/, or '# INPUT: <path>' lines in its TASK.md", from, from)
	}

	entry := ""
	for _, e := range entrypoints {
		if _, serr := os.Stat(filepath.Join(src, e.name)); serr == nil {
			b, rerr := os.ReadFile(filepath.Join(src, e.name))
			if rerr != nil {
				return rerr
			}
			if werr := os.WriteFile(filepath.Join(dst, e.name), b, 0o644); werr != nil {
				return werr
			}
			entry = e.name
			break
		}
	}
	if entry == "" {
		return fmt.Errorf("%s has no script this understands — expected one of: %s", from, entrypointNames())
	}

	note := fmt.Sprintf("# %s\n\nFrom: %s\n\n## What I am trying\n\n\n", slug, from)
	if werr := os.WriteFile(filepath.Join(dst, "RUN.md"), []byte(note), 0o644); werr != nil {
		return werr
	}

	fmt.Printf("%s/%s\n", runsDir, slug)
	fmt.Printf("  inputs/     %d file(s), copied from %s\n", copied, from)
	fmt.Printf("  %-11s yours to change\n", entry)
	fmt.Printf("  out/        empty; whatever the run writes here is the record's output\n\n")
	fmt.Printf("  edit it, then:  cockpit run %s\n", slug)
	return nil
}

func runExecute(ctx context.Context, slug string) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	rel := runsDir + "/" + slug
	dir := filepath.Join(cfg.RepoRoot, filepath.FromSlash(rel))
	if fi, serr := os.Stat(dir); serr != nil || !fi.IsDir() {
		return fmt.Errorf("no %s — make one with: cockpit run new %s --from <dir>", rel, slug)
	}

	entry, cmdOf := "", func(string) string { return "" }
	for _, e := range entrypoints {
		if _, serr := os.Stat(filepath.Join(dir, e.name)); serr == nil {
			entry, cmdOf = e.name, e.cmd
			break
		}
	}
	if entry == "" {
		return fmt.Errorf("%s has no script this understands — expected one of: %s", rel, entrypointNames())
	}

	inputs, err := filesUnderDir(cfg.RepoRoot, rel+"/inputs")
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return fmt.Errorf("%s/inputs is empty — a run with no inputs records a result from nothing", rel)
	}
	inputs = append(inputs, rel+"/"+entry)
	sort.Strings(inputs)

	// The previous attempt's outputs go before this one runs. Leaving them would make them outputs
	// of this record; asking the user to delete them is the chore this exists to remove.
	outDir := filepath.Join(dir, "out")
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	fmt.Printf("running %s in %s\n", rel+"/"+entry, cfg.Raw.Execution.Image)
	result, out, perr := tools.Publish(ctx, nil, tools.PublishInput{
		Cmd:       cmdOf(rel + "/" + entry),
		Inputs:    inputs,
		OutputDir: rel + "/out",
	})
	if perr != nil {
		return perr
	}
	if result != nil && result.IsError {
		for _, c := range result.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				fmt.Fprintln(os.Stderr, tc.Text)
			}
		}
		os.Exit(2)
	}

	if out.Stdout != "" {
		fmt.Println(indent(strings.TrimRight(out.Stdout, "\n"), "  | "))
	}
	fmt.Printf("\nfoton   %s\n", out.FotonID)
	fmt.Printf("ran in  %s\n", out.ExecutedIn)
	fmt.Printf("outputs %d\n", len(out.OutputHashes))
	for _, p := range sortedKeys(out.OutputHashes) {
		fmt.Printf("  %-52s %s\n", p, out.OutputHashes[p][:23]+"…")
	}
	if len(out.UndeclaredChanges) > 0 {
		fmt.Printf("\nthe run also changed %d file(s) outside %s/out — not recorded as outputs:\n",
			len(out.UndeclaredChanges), rel)
		for _, p := range out.UndeclaredChanges {
			fmt.Printf("  %s\n", p)
		}
	}
	if !out.Pushed {
		fmt.Printf("\nnot pushed. If a teammate pushed first: git pull --rebase && git push\n")
	}
	return nil
}

func runList(ctx context.Context) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(cfg.RepoRoot, runsDir))
	if os.IsNotExist(err) {
		fmt.Println("no runs yet — cockpit run new <slug> --from <dir>")
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		outs, _ := filesUnderDir(cfg.RepoRoot, runsDir+"/"+e.Name()+"/out")
		fmt.Printf("  %-28s %d output(s)\n", e.Name(), len(outs))
	}
	return nil
}

func entrypointNames() string {
	n := make([]string, 0, len(entrypoints))
	for _, e := range entrypoints {
		n = append(n, e.name)
	}
	return strings.Join(n, ", ")
}

// copyDeclaredInputs reads the '# INPUT: <repo-relative path>' lines an example's TASK.md carries
// and copies each one in. The example says what it starts from; nobody should have to read it and
// copy by hand.
func copyDeclaredInputs(repoRoot, src, dst string) (int, error) {
	b, err := os.ReadFile(filepath.Join(src, "TASK.md"))
	if err != nil {
		return 0, nil
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "# INPUT:") {
			continue
		}
		p := strings.TrimSpace(strings.TrimPrefix(line, "# INPUT:"))
		if p == "" {
			continue
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return n, err
		}
		if err := copyFile(filepath.Join(repoRoot, filepath.FromSlash(p)), filepath.Join(dst, filepath.Base(p))); err != nil {
			return n, fmt.Errorf("input %s: %w", p, err)
		}
		n++
	}
	return n, nil
}

func copyTree(src, dst string) (int, error) {
	n := 0
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
			return merr
		}
		n++
		return copyFile(p, target)
	})
	return n, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func filesUnderDir(repoRoot, dir string) ([]string, error) {
	root := filepath.Join(repoRoot, filepath.FromSlash(dir))
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, rerr := filepath.Rel(repoRoot, p)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out, err
}

func sortedKeys(m map[string]string) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}

func indent(s, p string) string {
	return p + strings.ReplaceAll(s, "\n", "\n"+p)
}
