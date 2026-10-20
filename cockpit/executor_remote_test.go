package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/executor"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// remote stands in for an executor that runs elsewhere (improve's run server): it writes the
// declared output into the repository, but a file the command deleted is only reported.
type remote struct{ got json.RawMessage }

func (r *remote) Run(_ context.Context, s executor.Step) (executor.Result, error) {
	r.got = s.Options
	if err := os.WriteFile(filepath.Join(s.Root, "data", "out.csv"), []byte("id,result\n1,84\n"), 0o644); err != nil {
		return executor.Result{}, err
	}
	return executor.Result{Image: s.Image, Removed: []string{"data/scratch.csv"}}, nil
}

func (r *remote) Validate(options json.RawMessage) error {
	var o struct {
		Server string `json:"server"`
	}
	if err := json.Unmarshal(options, &o); err != nil || o.Server == "" {
		return fmt.Errorf("needs server")
	}
	return nil
}

var fakeRemote = &remote{}

func init() { executor.Register("test-remote", fakeRemote) }

func remoteRepo(t *testing.T, options string) (*testrepo.Repo, error) {
	t.Helper()
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Execution = config.Execution{Executor: "test-remote", Image: testImageRef, Options: json.RawMessage(options)}
	r.WriteConfig(t, raw)
	r.Use(t)
	_, err := config.Load(context.Background(), r.Root)
	return r, err
}

const testImageRef = "oci://alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc"

// An executor's options reach it as written, and one it refuses stops the configuration loading.
func TestExecutor_OptionsReachTheExecutorAndAreValidatedAtLoad(t *testing.T) {
	if _, err := remoteRepo(t, `{"offline":true}`); err == nil || !strings.Contains(err.Error(), "execution.options") {
		t.Fatalf("a block the executor refuses must refuse the configuration: %v", err)
	}
	r, err := remoteRepo(t, `{"server":"https://run.example"}`)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	result, _, err := Publish(context.Background(), nil, PublishRequest{
		Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"}, Cmd: "double data/in.csv > data/out.csv",
	})
	if err != nil || result.IsError {
		t.Fatalf("publish: %v %+v", err, errText(result))
	}
	if !strings.Contains(string(fakeRemote.got), "run.example") {
		t.Fatalf("the executor got options %s", fakeRemote.got)
	}
}

// A deletion the executor reports is in the core's diff, although the file is still here.
func TestExecutor_ARemovalItReportsIsAnUndeclaredChange(t *testing.T) {
	r, err := remoteRepo(t, `{"server":"https://run.example"}`)
	if err != nil {
		t.Fatal(err)
	}
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	r.Write(t, "data/scratch.csv", "tmp\n")
	result, out, err := Publish(context.Background(), nil, PublishRequest{
		Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"}, Cmd: "double data/in.csv > data/out.csv; rm data/scratch.csv",
	})
	if err != nil || result.IsError {
		t.Fatalf("publish: %v %+v", err, errText(result))
	}
	if !slices.Contains(out.UndeclaredChanges, "data/scratch.csv (removed)") {
		t.Fatalf("the reported removal is missing from the undeclared changes: %v", out.UndeclaredChanges)
	}
}
