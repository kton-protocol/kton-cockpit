package cockpit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A two-step pipeline, run twice, recorded the way a session would record it: prep copies the
// data through, count takes its first N lines. The runs differ in their data and in N — so the
// proposal must offer the data as a file candidate and N as the one parameter candidate.
func pipelineRun(t *testing.T, r *testrepo.Repo, c *Cockpit, run string, data []string, n int) string {
	t.Helper()
	ctx := context.Background()
	prep, count := "work/"+run+"/prep", "work/"+run+"/count"
	r.Write(t, prep+"/prep.sh", "cp data.csv daten.txt\n")
	r.Write(t, prep+"/data.csv", strings.Join(data, "\n")+"\n")
	r.Write(t, prep+"/daten.txt", strings.Join(data, "\n")+"\n")
	if _, err := c.Publish(ctx, PublishRequest{
		Cmd:     "cd " + prep + " && sh prep.sh",
		Inputs:  []string{prep + "/prep.sh", prep + "/data.csv"},
		Outputs: []string{prep + "/daten.txt"},
	}); err != nil {
		t.Fatalf("%s prep: %v", run, err)
	}
	r.Write(t, count+"/count.sh", "head -n \"$1\" daten.txt > gezaehlt.txt\n")
	r.Write(t, count+"/daten.txt", strings.Join(data, "\n")+"\n")
	r.Write(t, count+"/gezaehlt.txt", strings.Join(data[:n], "\n")+"\n")
	out, err := c.Publish(ctx, PublishRequest{
		Cmd:     fmt.Sprintf("cd %s && sh count.sh %d", count, n),
		Inputs:  []string{count + "/count.sh", count + "/daten.txt"},
		Outputs: []string{count + "/gezaehlt.txt"},
	})
	if err != nil {
		t.Fatalf("%s count: %v", run, err)
	}
	return out.OutputHashes[count+"/gezaehlt.txt"]
}

func rayRepo(t *testing.T) (*testrepo.Repo, *Cockpit, []string) {
	t.Helper()
	r := testrepo.New(t)
	raw := r.Config(t).Raw
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "derived-from")
	r.WriteConfig(t, raw)
	c := New(Start{Dir: r.Root})
	e1 := pipelineRun(t, r, c, "r1", []string{"a", "b", "c", "d"}, 3)
	e2 := pipelineRun(t, r, c, "r2", []string{"x", "y"}, 1)
	return r, c, []string{e1, e2}
}

func TestRay_TheProposalOffersWhatTheRunsShowAndDecidesNothing(t *testing.T) {
	_, c, ends := rayRepo(t)
	out, err := c.Ask(context.Background(), AskRequest{Query: "ray", Refs: ends})
	if err != nil {
		t.Fatal(err)
	}
	p := out.Proposal
	if p == nil || len(p.Runs) != 2 || len(p.Steps) != 2 || len(p.Conflicts) != 0 {
		t.Fatalf("2 runs, 2 steps, no conflicts expected, got %+v", p)
	}
	files := map[string]bool{}
	for _, f := range p.Files {
		files[fmt.Sprintf("%s/%s code=%v", f.Step, f.Slot, f.CommandFile)] = true
	}
	for _, want := range []string{"prep/data.csv code=false", "prep/prep.sh code=true", "count/count.sh code=true"} {
		if !files[want] {
			t.Errorf("file candidate missing: %s (have %v)", want, files)
		}
	}
	if len(p.Params) != 1 || p.Params[0].Step != "count" || p.Params[0].Position != 2 {
		t.Fatalf("exactly one parameter candidate expected (count, position 2), got %+v", p.Params)
	}
	if len(out.Included) != 4 {
		t.Fatalf("the four verified records expected, got %v", out.Included)
	}
}

func rayChoice(p *ktonpkg.Proposal, ref string) ktonpkg.Choice {
	return ktonpkg.Choice{Name: "zaehlen", Reference: ref,
		Holes:  []ktonpkg.HoleChoice{{Name: "data", Step: "prep", Slot: "data.csv"}},
		Params: []ktonpkg.ParamChoice{{Name: "n", Step: "count", Position: 2}}}
}

func referenceRun(t *testing.T, p *ktonpkg.Proposal, endpoint string) string {
	t.Helper()
	for _, r := range p.Runs {
		for _, e := range r.Endpoints {
			if e == endpoint {
				return r.ID
			}
		}
	}
	t.Fatalf("no run ends in %s", endpoint)
	return ""
}

func TestRay_PublishWritesThePackageAndRecordsWhereItCameFrom(t *testing.T) {
	r, c, ends := rayRepo(t)
	ctx := context.Background()
	prop, err := c.Ask(ctx, AskRequest{Query: "ray", Refs: ends})
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Publish(ctx, PublishRequest{Kind: "ray", Ray: &RayPublish{
		Refs: ends, Dir: "packages/zaehlen",
		Choice: rayChoice(prop.Proposal, referenceRun(t, prop.Proposal, ends[0])),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Ray == nil || !strings.HasPrefix(out.Ray.RayID, "sha256:") || len(out.Ray.DerivedFrom) != 2 {
		t.Fatalf("a ray with two source records expected, got %+v", out.Ray)
	}
	b, err := ktonpkg.Open(filepath.Join(r.Root, "packages", "zaehlen"))
	if err != nil {
		t.Fatal(err)
	}
	if problems := b.Verify(); len(problems) > 0 {
		t.Fatalf("the package does not pass its own check: %v", problems)
	}
	if len(b.Ray.Holes) != 2 {
		t.Fatalf("the two chosen holes expected, got %+v", b.Ray.Holes)
	}
	if got := b.Ray.Step("count").Protocol.Command.Args; got != "sh\n<command-file>\n<n>" {
		t.Fatalf("the command line with its parameter expected, got %q", got)
	}
	if !r.OriginContains(t, out.CommitSHA) {
		t.Fatal("the package and its claim were not pushed")
	}
	about, err := c.Ask(ctx, AskRequest{Query: "about", Ref: out.Ray.RayID})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cl := range about.Claims {
		if cl.ID == out.Ray.ClaimID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the derived-from claim %s is not among the verified claims about the ray: %+v", out.Ray.ClaimID, about.Claims)
	}
}

func TestRay_PublishRefusesWhatWasNotOffered(t *testing.T) {
	r, c, ends := rayRepo(t)
	ctx := context.Background()
	prop, _ := c.Ask(ctx, AskRequest{Query: "ray", Refs: ends})
	ref := referenceRun(t, prop.Proposal, ends[0])

	bad := rayChoice(prop.Proposal, ref)
	bad.Params = []ktonpkg.ParamChoice{{Name: "n", Step: "prep", Position: 1}}
	_, err := c.Publish(ctx, PublishRequest{Kind: "ray", Ray: &RayPublish{Refs: ends, Dir: "packages/a", Choice: bad}})
	if ref := refusalOf(t, err); ref.Code != "ray.choice" {
		t.Fatalf("a parameter nobody's runs varied must be refused as ray.choice, got %q: %s", ref.Code, ref.Reason)
	}
	if _, statErr := os.Stat(filepath.Join(r.Root, "packages", "a")); statErr == nil {
		t.Fatal("a refused extraction left a directory behind")
	}

	r.Write(t, "packages/b/x", "da\n")
	_, err = c.Publish(ctx, PublishRequest{Kind: "ray", Ray: &RayPublish{Refs: ends, Dir: "packages/b",
		Choice: rayChoice(prop.Proposal, ref)}})
	if ref := refusalOf(t, err); ref.Code != "ray.exists" {
		t.Fatalf("an existing directory must be refused as ray.exists, got %q", ref.Code)
	}
}

func TestRay_PublishNeedsTheTemplateAdmitted(t *testing.T) {
	r := testrepo.New(t)
	c := New(Start{Dir: r.Root})
	_, err := c.Publish(context.Background(), PublishRequest{Kind: "ray", Ray: &RayPublish{
		Refs: []string{unknownHash}, Dir: "packages/x", Choice: ktonpkg.Choice{Name: "x"}}})
	if ref := refusalOf(t, err); ref.Code != "template.not-allowed" {
		t.Fatalf("without derived-from admitted, a ray must be refused, got %q", ref.Code)
	}
}

// With an entrypoint configured, a recorded shell line takes a package's form: the image first,
// the entrypoint dropped. A line that does not start with it is refused, not rewritten.
func TestRay_TheAdapterTranslatesOnlyTheNamedEntrypoint(t *testing.T) {
	rec := &binaries.FotonDetail{ID: "sha256:x", Cmd: "cd work/s && Rscript s.R 3",
		EnvRef: "oci://docker.io/scinteco/jam-r@sha256:00",
		Inputs: []binaries.FotonFile{{Path: "work/s/s.R", Hash: "sha256:1"}}}
	e, err := executionOf(rec, "Rscript")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(e.Command, "|"); got != "scinteco/jam-r@sha256:00|s.R|3" || e.CommandFile != "s.R" {
		t.Fatalf("the package form expected, got %q (command file %q)", got, e.CommandFile)
	}
	if _, err := executionOf(rec, "python"); err == nil {
		t.Fatal("a command line that does not start with the named entrypoint must be refused")
	}
}
