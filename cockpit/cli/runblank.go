package cli

// runblank.go is `cockpit run new <slug> --in FILE...`: a run folder started from data rather than
// from an earlier run. The script it writes runs as it is — it lists its inputs into out/ — so the
// first `cockpit run <slug>` already records something, and the person changes it from there.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

var starterScripts = map[string]string{
	"run.py": `# Reads inputs/, writes out/. ` + "`cockpit run <slug>`" + ` runs it here, in the pinned image.
import pathlib

inputs = sorted(pathlib.Path("inputs").iterdir())
pathlib.Path("out/summary.txt").write_text("".join(f"{p.name}\t{p.stat().st_size}\n" for p in inputs))
`,
	"run.R": `# Reads inputs/, writes out/. ` + "`cockpit run <slug>`" + ` runs it here, in the pinned image.
inputs <- sort(list.files("inputs", full.names = TRUE))
writeLines(paste(basename(inputs), file.size(inputs), sep = "\t"), "out/summary.txt")
`,
	"run.sh": `# Reads inputs/, writes out/. ` + "`cockpit run <slug>`" + ` runs it here, in the pinned image.
for f in inputs/*; do printf '%s\t%s\n' "$(basename "$f")" "$(wc -c < "$f")"; done > out/summary.txt
`,
}

func runNewBlank(ctx context.Context, slug string, ins []string, script string) error {
	if script == "" {
		script = "run.py"
	}
	if _, ok := starterScripts[script]; !ok {
		names := make([]string, 0, len(starterScripts))
		for n := range starterScripts {
			names = append(names, n)
		}
		slices.Sort(names)
		return fmt.Errorf("--script is one of %v", names)
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
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

	if err := os.MkdirAll(filepath.Join(dst, "out"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dst, "inputs"), 0o755); err != nil {
		return err
	}
	seen := map[string]string{}
	for _, in := range ins {
		name := filepath.Base(in)
		if prev, dup := seen[name]; dup {
			return fmt.Errorf("%s and %s would both be inputs/%s", prev, in, name)
		}
		seen[name] = in
		if err := copyFile(in, filepath.Join(dst, "inputs", name)); err != nil {
			return fmt.Errorf("--in %s: %w", in, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dst, script), []byte(starterScripts[script]), 0o644); err != nil {
		return err
	}
	note := fmt.Sprintf("# %s\n\nFrom: %d input file(s)\n\n## What I am trying\n\n\n", slug, len(ins))
	if err := os.WriteFile(filepath.Join(dst, "RUN.md"), []byte(note), 0o644); err != nil {
		return err
	}
	made = true

	fmt.Printf("%s/%s\n", runsDir, slug)
	fmt.Printf("  inputs/     %d file(s)\n", len(ins))
	fmt.Printf("  %-11s a start that runs as it is: it lists inputs/ into out/summary.txt\n", script)
	fmt.Printf("  out/        empty; whatever the run writes here is the record's output\n\n")
	fmt.Printf("  edit it, then:  cockpit run %s\n", slug)
	return nil
}
