//go:build docker

package cockpit

import (
	"context"
	"testing"
)

// Where the cockpit runs every command (execution.image), a declared step is still not run: it is
// recorded as declared. The same command, observed, is run and fails (MH, 21.11.).
func TestContainer_ADeclaredStepIsNotRun(t *testing.T) {
	r := executingRepo(t, false)
	r.Write(t, "data/in.csv", "id\n1\n")
	r.Write(t, "data/out.csv", "id\n2\n")
	req := PublishRequest{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"}, Cmd: "exit 3"}
	if _, err := New(Start{}).Publish(context.Background(), req); err == nil {
		t.Fatal("observed, the command runs and fails")
	}
	req.Declared = "hand"
	out, err := New(Start{}).Publish(context.Background(), req)
	if err != nil || out.FotonID == "" {
		t.Errorf("declared, the step is recorded without running: %v", err)
	}
}
