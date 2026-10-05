package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"kton.dev/plankton/core"
)

// A record `cockpit run` made is laid out again as a run folder, so the clone runs the same command.
func TestLayoutOf_ARunFolderStaysOne(t *testing.T) {
	rec := &binaries.FotonDetail{ID: "sha256:aa", Cmd: "cd runs/first && python3 run.py",
		Inputs: []binaries.FotonFile{{Path: "runs/first/inputs/in.csv"}, {Path: "runs/first/run.py"}}}
	files, script, body := layoutOf(rec)
	if script != "run.py" || body != "" {
		t.Fatalf("script %q body %q", script, body)
	}
	if files[0].to != "inputs/in.csv" || files[1].to != "run.py" {
		t.Errorf("placed at %q, %q", files[0].to, files[1].to)
	}
}

// Any other record keeps its paths under inputs/, and run.sh moves its outputs to out/ — moved, so a
// second run does not count them as inputs.
func TestLayoutOf_AnyOtherRecordGetsARunScript(t *testing.T) {
	rec := &binaries.FotonDetail{ID: "sha256:bb", Cmd: "python3 session-1/clean.py",
		Inputs:  []binaries.FotonFile{{Path: "data/runs.csv"}, {Path: "session-1/clean.py"}},
		Outputs: []binaries.FotonFile{{Path: "session-1/clean.csv"}}}
	files, script, body := layoutOf(rec)
	if script != "run.sh" || files[0].to != "inputs/data/runs.csv" {
		t.Fatalf("script %q, first at %q", script, files[0].to)
	}
	for _, want := range []string{"cd inputs\n", "python3 session-1/clean.py\n", `mv "session-1/clean.csv" ../out/`} {
		if !strings.Contains(body, want) {
			t.Errorf("run.sh lacks %q:\n%s", want, body)
		}
	}
}

// A locator that cannot be fetched over HTTP is tried as git at the pinned commit, and bytes are
// taken only when they hash to what the record names.
func TestFetch_GitAtThePinnedCommitAndOnlyTheRecordedBytes(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "origin.git")
	work := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	must(t, exec.Command("git", "init", "-q", "--bare", "-b", "main", bare).Run())
	git(work, "init", "-q", "-b", "main")
	must(t, os.WriteFile(filepath.Join(work, "a.txt"), []byte("first\n"), 0o644))
	git(work, "add", "a.txt")
	git(work, "commit", "-qm", "1")
	sha := git(work, "rev-parse", "HEAD")
	git(work, "push", "-q", bare, "main")

	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+bare+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/someone/somewhere.git")
	uri := "https://raw.githubusercontent.com/someone/somewhere/" + sha + "/a.txt"

	f := newFetcher()
	defer f.close()
	f.client.Timeout = 1 // no network in a test: the HTTP attempt fails at once
	b, _, err := f.fetch(context.Background(), binaries.FotonFile{Path: "a.txt", Hash: core.HashBytes([]byte("first\n")), URI: []string{uri}})
	if err != nil || string(b) != "first\n" {
		t.Fatalf("got %q, %v", b, err)
	}
	// The negative control: the same locator, a different recorded hash.
	if _, _, err := f.fetch(context.Background(), binaries.FotonFile{Path: "a.txt", Hash: core.HashBytes([]byte("other\n")), URI: []string{uri}}); err == nil {
		t.Error("bytes that do not match the recorded hash were accepted")
	}
}

func TestReproducesOf(t *testing.T) {
	p := filepath.Join(t.TempDir(), "RUN.md")
	must(t, os.WriteFile(p, []byte("# x\n\nReproduces: sha256:abc\nSigned by: keyid k\n"), 0o644))
	if got := reproducesOf(p); got != "sha256:abc" {
		t.Errorf("got %q", got)
	}
	must(t, os.WriteFile(p, []byte("# x\n\nFrom: runs/first\n"), 0o644))
	if got := reproducesOf(p); got != "" {
		t.Errorf("a run folder from a folder names %q", got)
	}
}
