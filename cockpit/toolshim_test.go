package cockpit

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
	"github.com/kton-protocol/kton-cockpit/internal/toolcall"
)

// shimRepo is a repository with one shim configured; image is never run by these tests.
func shimRepo(t *testing.T, shims map[string]config.Shim) *Cockpit {
	t.Helper()
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Shims = shims
	r.WriteConfig(t, raw)
	r.Use(t)
	return New(Start{})
}

const shimImage = "oci://alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc"

// Nothing the shim sends is trusted: a path that could leave the staging directory is refused
// before anything is staged or run (ADR-014, section 8, D1). So is a tool no shim configures, and a call
// collected by another collector than the configured one.
func TestShim_ACallThatCouldLeaveTheStagingDirectoryIsRefused(t *testing.T) {
	c := shimRepo(t, map[string]config.Shim{"cp": {Collector: "files", Image: shimImage}})
	file := func(p string) []CallFile { return []CallFile{{Path: p, Data: []byte("x")}} }
	for name, call := range map[string]ToolCall{
		"absolute":       {Files: file("/etc/passwd")},
		"dot-dot":        {Files: file("../outside")},
		"dot-dot inside": {Files: file("data/../../outside")},
		"NUL":            {Files: file("data/a\x00b")},
		"empty segment":  {Files: file("data//a")},
		"dot":            {Files: file("./a")},
		"empty":          {Files: file("")},
		"streams":        {Files: file(".kton/exit-code")},
		"twice":          {Files: append(file("a"), file("a")...)},
		"cwd outside":    {Cwd: "../x", Files: file("a")},
		"NUL argument":   {Args: []string{"a\x00b"}, Files: file("a")},
	} {
		call.Tool, call.Collector = "cp", "files"
		_, err := c.Call(context.Background(), call)
		var ref *Refusal
		if !errors.As(err, &ref) || ref.Code != "shim.path" {
			t.Errorf("%s: want refusal shim.path, got %v", name, err)
		}
	}
	if _, err := c.Call(context.Background(), ToolCall{Tool: "rm", Collector: "files"}); !isRefusal(err, "shim.unknown") {
		t.Errorf("an unconfigured tool: %v", err)
	}
	if _, err := c.Call(context.Background(), ToolCall{Tool: "cp", Collector: "nonmem"}); !isRefusal(err, "shim.collector") {
		t.Errorf("another collector: %v", err)
	}
}

func isRefusal(err error, code string) bool {
	var ref *Refusal
	return errors.As(err, &ref) && ref.Code == code
}

func TestShim_CheckRelAcceptsOnlyCleanRelativePaths(t *testing.T) {
	for _, p := range []string{"a", "data/pk.csv", "models/run1/run1.mod", "a.b/c-d_e"} {
		if err := toolcall.CheckRel(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	if got := expandArgs("$2.lst", []string{"run1.mod", "run1"}); got != "run1.lst" {
		t.Errorf("expandArgs: %q", got)
	}
}

// A call is keyed relative to its base, the deepest directory holding its directory and its
// inputs: the same call in another directory (pharmpy's NONMEM_run_<model>-<uuid>) is the same
// call, and the outputs go back under the base it came from.
func TestShim_ACallIsKeyedRelativeToItsBase(t *testing.T) {
	file := func(p string) CallFile { return CallFile{Path: p} }
	for _, tc := range []struct {
		cwd   string
		args  []string
		files []string
		base  string
		rcwd  string
		paths []string
	}{
		{"run-1/x", nil, []string{"run-1/x/model.ctl", "run-1/.datasets/d.csv"}, "run-1", "x", []string{"x/model.ctl", ".datasets/d.csv"}},
		{"run-1/x", nil, []string{"run-1/x/model.ctl"}, "run-1/x", "", []string{"model.ctl"}},
		{"models", nil, []string{"models/m.ctl", "data/d.csv"}, "", "models", []string{"models/m.ctl", "data/d.csv"}},
		{"", nil, []string{"m.ctl"}, "", "", []string{"m.ctl"}},
		{"ab/c", nil, []string{"ab/c/m.ctl", "a/d.csv"}, "", "ab/c", []string{"ab/c/m.ctl", "a/d.csv"}},
		// an argument that reaches above the call's directory moves the base up (an output ../out.csv)
		{"m/r1", []string{"in.csv", "../out.csv"}, []string{"m/r1/in.csv"}, "m", "r1", []string{"r1/in.csv"}},
		{"m/r1", []string{"-o=../../out.csv", "in.csv"}, []string{"m/r1/in.csv"}, "", "m/r1", []string{"m/r1/in.csv"}},
		{"m/r1", []string{"../../../out.csv"}, []string{"m/r1/in.csv"}, "", "m/r1", []string{"m/r1/in.csv"}},
	} {
		in := ToolCall{Tool: "nmfe75", Cwd: tc.cwd, Args: tc.args}
		for _, p := range tc.files {
			in.Files = append(in.Files, file(p))
		}
		out, base := rebase(in)
		var paths []string
		for _, f := range out.Files {
			paths = append(paths, f.Path)
		}
		if base != tc.base || out.Cwd != tc.rcwd || !slices.Equal(paths, tc.paths) {
			t.Errorf("rebase(%q, %q) = %q, %q, %q; want %q, %q, %q", tc.cwd, tc.files, base, out.Cwd, paths, tc.base, tc.rcwd, tc.paths)
		}
		if in.Files[0].Path != tc.files[0] {
			t.Errorf("rebase changed the caller's call")
		}
	}
}
