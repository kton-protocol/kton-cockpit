//go:build docker

package cockpit

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// Outputs the cockpit finds by running — outputDir, which is what `cockpit run` uses — are
// committed and located like named ones. They were neither: a run's result was recorded by hash
// alone, so nobody could fetch its bytes from the commit the record names.
func TestContainer_OutputDirOutputsAreCommittedAndLocated(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "id,value\n1,42\n")

	result, out, err := Publish(context.Background(), nil, PublishRequest{
		Inputs:    []string{"data/in.csv"},
		OutputDir: "work/out",
		Cmd:       "mkdir -p work/out && cp data/in.csv work/out/copy.csv",
	})
	if err != nil || result.IsError {
		t.Fatalf("publish failed: %v %s", err, errText(result))
	}
	if _, ok := out.Permalinks["work/out/copy.csv"]; !ok {
		t.Errorf("no permalink for the output the run wrote: %v", out.Permalinks)
	}
	tracked, _ := exec.Command("git", "-C", r.Root, "ls-files", "work/out").Output()
	if !strings.Contains(string(tracked), "work/out/copy.csv") {
		t.Errorf("the output the run wrote is not committed (git ls-files: %q)", tracked)
	}
	// The negative control: the input was always committed; it still is, once.
	if _, ok := out.Permalinks["data/in.csv"]; !ok {
		t.Error("the input lost its permalink")
	}
}
