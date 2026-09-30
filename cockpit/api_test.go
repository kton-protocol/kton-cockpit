package cockpit

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// These drive the package the way a caller in another program does: a Cockpit value, a request,
// and an error that is either a *Refusal or not. The rest of this package's tests go through the
// MCP-shaped shim, which says whether something was refused but not which refusal it was.

func refusalOf(t *testing.T, err error) *Refusal {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("expected a *Refusal, got %T: %v", err, err)
	}
	return r
}

// Start.Dir is where the search for cockpit.config.json begins, independently of the process's
// working directory — the whole point of a library is that its caller need not chdir.
func TestAPI_StartDirIsWhereTheSearchBegins(t *testing.T) {
	r := testrepo.New(t)
	chdir(t, os.TempDir()) // the process stands somewhere with no configuration at all
	r.Write(t, "work/out.csv", "a,b\n1,2\n")

	out, err := New(Start{Dir: r.Root}).Publish(context.Background(), PublishRequest{
		Cmd: "produce work/out.csv", Outputs: []string{"work/out.csv"},
	})
	if err != nil {
		t.Fatalf("publish from Start.Dir failed: %v", err)
	}
	if !strings.HasPrefix(out.FotonID, "sha256:") {
		t.Fatalf("no foton id: %+v", out)
	}
}

// A refusal carries a stable code and the clause it enforces, so a program can act on which
// refusal it met without reading prose. The binding refusal is the one this project exists for.
func TestAPI_ARefusalCarriesItsCodeAndClause(t *testing.T) {
	chdir(t, os.TempDir())
	_, err := New(Start{}).Ask(context.Background(), AskRequest{Query: "producer", Ref: unknownHash})
	ref := refusalOf(t, err)
	if ref.Code != "binding" || ref.Clause != "SPEC §5" {
		t.Fatalf("outside any repository: got code %q clause %q, want binding / SPEC §5 (reason: %s)",
			ref.Code, ref.Clause, ref.Reason)
	}
	if ref.Error() != ref.Reason {
		t.Fatalf("Error() must be the reason alone, as a person reads it: %q vs %q", ref.Error(), ref.Reason)
	}
}

func TestAPI_ATemplateOutsideTheCeilingIsRefusedByCode(t *testing.T) {
	r := testrepo.New(t)
	_, err := New(Start{Dir: r.Root}).Say(context.Background(), SayRequest{
		Subject: unknownHash, Template: "not-a-configured-template",
	})
	if ref := refusalOf(t, err); ref.Code != "template.not-allowed" || ref.Clause != "SPEC §8.1" {
		t.Fatalf("got code %q clause %q, want template.not-allowed / SPEC §8.1", ref.Code, ref.Clause)
	}
}

func TestAPI_ADeniedPathIsRefusedByCode(t *testing.T) {
	r := testrepo.New(t)
	_, err := New(Start{Dir: r.Root}).Publish(context.Background(), PublishRequest{
		Cmd: "leak", Outputs: []string{"cockpit.config.json"},
	})
	if ref := refusalOf(t, err); ref.Code != "path.denied" || ref.Clause != "SPEC §7.3" {
		t.Fatalf("got code %q clause %q, want path.denied / SPEC §7.3", ref.Code, ref.Clause)
	}
}

// A repository configured for a mode this binary does not have is refused by name. Acting as git —
// the old default for anything unrecognised — would be acting as another backend against a
// repository configured for this one (ADR-008).
func TestAPI_AModeNoBackendProvidesIsRefusedByName(t *testing.T) {
	r := testrepo.New(t)
	raw := r.Config(t).Raw
	raw.Repo.Mode, raw.Repo.Block = "improve", nil
	r.WriteConfig(t, raw)
	_, err := New(Start{Dir: r.Root}).Ask(context.Background(), AskRequest{Query: "producer", Ref: unknownHash})
	ref := refusalOf(t, err)
	if ref.Code != "binding" || !strings.Contains(ref.Reason, `repo.mode "improve" is not a mode this cockpit knows`) {
		t.Fatalf("an unknown mode must be refused by name, got %q: %s", ref.Code, ref.Reason)
	}
}
