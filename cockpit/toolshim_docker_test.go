//go:build docker

package cockpit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// A call no record answers runs in the tool's image on exactly the staged inputs and is recorded;
// the same call again is answered from that record without running, with the same outputs, streams
// and exit code (ADR-014).
func TestShim_AMissRunsInTheToolsImageAndTheSameCallIsAHit(t *testing.T) {
	t.Setenv("KTON_CAS", t.TempDir())
	c := shimRepo(t, map[string]config.Shim{"cp": {Collector: "files", Image: shimImage, Expect: []string{"$2"}}})
	call := ToolCall{Tool: "cp", Collector: "files", Cwd: "models", Args: []string{"../data/in.csv", "out.csv"},
		Files: []CallFile{{Path: "data/in.csv", Data: []byte("id,v\n1,42\n")}}}

	first, err := c.Call(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused || first.FotonID == "" || first.ExitCode != 0 {
		t.Fatalf("a miss: want it run and recorded: %+v", first)
	}
	if len(first.Files) != 1 || first.Files[0].Path != "models/out.csv" || string(first.Files[0].Data) != "id,v\n1,42\n" {
		t.Fatalf("the output, relative to the repository root: %+v", first.Files)
	}

	again, err := c.Call(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Reused || again.FotonID != first.FotonID || again.ExitCode != 0 ||
		len(again.Files) != 1 || string(again.Files[0].Data) != "id,v\n1,42\n" {
		t.Fatalf("the same call: want it answered from %s: %+v", first.FotonID, again)
	}

	call.Files[0].Data = []byte("id,v\n1,43\n")
	other, err := c.Call(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if other.Reused || other.FotonID == first.FotonID {
		t.Fatalf("other input bytes are another call: %+v", other)
	}
}

// A run that leaves a link is not read or recorded: otherwise out -> a host file would be read,
// signed and handed back (ADR-014, section 8, D2).
func TestShim_ARunThatLeavesALinkIsNotReadOrRecorded(t *testing.T) {
	t.Setenv("KTON_CAS", t.TempDir())
	c := shimRepo(t, map[string]config.Shim{"ln": {Collector: "files", Image: shimImage}})
	_, err := c.Call(context.Background(), ToolCall{Tool: "ln", Collector: "files", Args: []string{"-s", "/etc/hostname", "out"},
		Files: []CallFile{{Path: "in", Data: []byte("x")}}})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("want the run refused for its link: %v", err)
	}
}

// A secret is mounted read-only and never recorded: a run whose output contains it ran, and its
// outputs come back, but nothing is signed (D3, a tripwire). A run whose exit code is no result, or
// that did not write what a result always has, is not recorded either.
func TestShim_ASecretOrAFailureIsNotRecorded(t *testing.T) {
	t.Setenv("KTON_CAS", t.TempDir())
	secret := filepath.Join(t.TempDir(), "nonmem.lic")
	os.WriteFile(secret, []byte("LICENSED-TO-SOMEONE"), 0o600)
	c := shimRepo(t, map[string]config.Shim{
		"cp": {Collector: "files", Image: shimImage, Secrets: []config.ShimSecret{{Host: secret, At: "/opt/lic"}}, Expect: []string{"$2"}},
	})
	res, err := c.Call(context.Background(), ToolCall{Tool: "cp", Collector: "files", Args: []string{"/opt/lic", "out"},
		Files: []CallFile{{Path: "in", Data: []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.FotonID != "" || !strings.Contains(res.Note, "secret") {
		t.Fatalf("a copied secret: want it not recorded: %+v", res)
	}

	res, err = c.Call(context.Background(), ToolCall{Tool: "cp", Collector: "files", Args: []string{"missing", "out"},
		Files: []CallFile{{Path: "in", Data: []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.FotonID != "" || res.ExitCode == 0 || !strings.Contains(res.Note, "exit code") || len(res.Stderr) == 0 {
		t.Fatalf("a failed run: want its exit code and stderr back, nothing recorded: %+v", res)
	}
}

// A call whose output lies above its directory still runs and is recorded: the base it is keyed
// relative to holds whatever its arguments reach (review S1).
func TestShim_AnOutputAboveTheCallsDirectoryIsWritten(t *testing.T) {
	t.Setenv("KTON_CAS", t.TempDir())
	c := shimRepo(t, map[string]config.Shim{"cp": {Collector: "files", Image: shimImage, Expect: []string{"$2"}}})
	call := ToolCall{Tool: "cp", Collector: "files", Cwd: "m/r1", Args: []string{"in.csv", "../out.csv"},
		Files: []CallFile{{Path: "m/r1/in.csv", Data: []byte("x")}}}
	res, err := c.Call(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.FotonID == "" || len(res.Files) != 1 || res.Files[0].Path != "m/out.csv" {
		t.Fatalf("want m/out.csv written and recorded: %+v", res)
	}
	if again, err := c.Call(context.Background(), call); err != nil || !again.Reused || len(again.Files) != 1 || again.Files[0].Path != "m/out.csv" {
		t.Fatalf("the same call: want a hit with m/out.csv: %+v %v", again, err)
	}
}

// What a run leaves under .kton/ besides its streams is its own: it comes back neither on a miss
// nor on a hit (review S2).
func TestShim_FilesUnderTheStreamsDirectoryNeverComeBack(t *testing.T) {
	t.Setenv("KTON_CAS", t.TempDir())
	c := shimRepo(t, map[string]config.Shim{"sh": {Collector: "files", Image: shimImage}})
	call := ToolCall{Tool: "sh", Collector: "files", Args: []string{"-c", "echo x >.kton/extra; echo o >o"},
		Files: []CallFile{{Path: "in", Data: []byte("x")}}}
	for _, what := range []string{"miss", "hit"} {
		res, err := c.Call(context.Background(), call)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Files) != 1 || res.Files[0].Path != "o" {
			t.Fatalf("%s: want only o back: %+v", what, res.Files)
		}
	}
}

// A run that leaves more than the shim's cap is not read or recorded, whatever it costs on disk: a
// sparse file is checked by its size, before anything is read (review S3).
func TestShim_ARunThatLeavesTooMuchIsNotRead(t *testing.T) {
	t.Setenv("KTON_CAS", t.TempDir())
	c := shimRepo(t, map[string]config.Shim{"truncate": {Collector: "files", Image: shimImage, MaxOutputBytes: 1 << 20}})
	_, err := c.Call(context.Background(), ToolCall{Tool: "truncate", Collector: "files", Args: []string{"-s", "100G", "big"},
		Files: []CallFile{{Path: "in", Data: []byte("x")}}})
	if err == nil || !strings.Contains(err.Error(), "maxOutputBytes") {
		t.Fatalf("want the run refused for its size: %v", err)
	}
}
