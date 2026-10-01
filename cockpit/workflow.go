package cockpit

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg"
	"github.com/gitmick/ktonpkg/scope"
	pregistry "kton.dev/plankton/registry"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// Workflows: from runs that already happened to a package, and from an installed package to runs.
//
//	propose  what the runs behind some results offer: steps, files that could be holes, places on a
//	         command line that could be parameters (ktonpkg.Propose, nothing decided)
//	extract  the chosen potential as a package in the scope format, with the runs of the reference
//	         run as its reference runs, sealed
//	list     the installed workflows (SPARQL query "workflows" from the allowed vocabulary)
//	show     one installed workflow: steps, holes, parameters, reference
//	run      perform it: bind, stage, publish step by step; --check runs it with its test data and
//	         says whether this repository reproduces the reference

// ---- propose ---------------------------------------------------------------------------------------

// WorkflowPropose is ask {query: "ray"} under its own name.
func (c *Cockpit) WorkflowPropose(ctx context.Context, refs []string) (*AskResult, error) {
	return c.Ask(ctx, AskRequest{Query: "ray", Refs: refs})
}

// ---- extract ---------------------------------------------------------------------------------------

// WorkflowExtractRequest is what extract needs: the results, and what was chosen from the proposal.
type WorkflowExtractRequest struct {
	Refs   []string       `json:"refs"`
	Choice ktonpkg.Choice `json:"choice"`
	// Label is the revision; "1" when empty.
	Label string `json:"label,omitempty"`
}

// WorkflowExtractResult reports the package written.
type WorkflowExtractResult struct {
	Dir        string   `json:"dir"`
	Package    string   `json:"package"`
	Revision   string   `json:"revision"`
	Ray        string   `json:"ray"`
	Seal       string   `json:"seal"`
	References []string `json:"references"` // the reference runs packed with it
	Holes      []string `json:"holes,omitempty"`
	Params     []string `json:"params,omitempty"`
	CommitSHA  string   `json:"commitSha,omitempty"`
}

// WorkflowExtract writes the chosen potential as a package in the scope format under
// packages/<name>/: the ray as potentials, the test data from the reference run, and the reference
// run's executions as reference runs — the outputs a user re-runs the workflow against.
func (c *Cockpit) WorkflowExtract(ctx context.Context, in WorkflowExtractRequest) (*WorkflowExtractResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	if in.Choice.Name == "" {
		return nil, refuse("argument", "", "a workflow needs a name")
	}
	label := in.Label
	if label == "" {
		label = "1"
	}
	rel := path.Join(packages.Dir, in.Choice.Name)
	abs := filepath.Join(cfg.RepoRoot, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err == nil {
		return nil, refuse("workflow.exists", "", "%s already exists — a package is written new; a further revision is a separate step", rel)
	}

	r := binaries.New(cfg)
	execs, where, _, err := rayExecutions(ctx, cfg, r, in.Refs, &AskFilter{})
	if err != nil {
		return nil, err
	}
	prop, err := ktonpkg.Propose(execs)
	if err != nil {
		return nil, refuse("ray.propose", "SPEC §9.7", "%v", err)
	}
	// The reference may be named by a run of the proposal, or — what a person knows — by one of
	// the results given: the run that ended in it.
	run := ""
	refHash := in.Choice.Reference
	if !strings.HasPrefix(refHash, "sha256:") {
		if h, _, herr := hashLocalFile(ctx, cfg, r, refHash); herr == nil {
			refHash = h
		}
	}
	for _, ru := range prop.Runs {
		if ru.ID == in.Choice.Reference {
			run = ru.ID
		}
		for _, e := range ru.Endpoints {
			if e == refHash {
				run = ru.ID
			}
		}
	}
	if run == "" {
		return nil, refuse("ray.choice", "SPEC §7.6",
			"the reference %q is neither a run of the proposal nor one of the results given", in.Choice.Reference)
	}
	in.Choice.Reference = run
	fetch := func(hash string) ([]byte, error) {
		p, ok := where[hash]
		if !ok {
			return nil, fmt.Errorf("no file with this hash among the selected records")
		}
		return os.ReadFile(filepath.Join(cfg.RepoRoot, filepath.FromSlash(p)))
	}
	tmp, err := os.MkdirTemp("", "workflow-extract-")
	if err != nil {
		return nil, refuse("io", "", "%v", err)
	}
	defer os.RemoveAll(tmp)
	b, err := ktonpkg.Extract(prop, execs, in.Choice, fetch, filepath.Join(tmp, "bundle"))
	if err != nil {
		return nil, refuse("ray.choice", "SPEC §7.6", "%v", err)
	}

	key, err := loadPrivate(cfg.NektonKey)
	if err != nil {
		return nil, refuse("identity", "", "%v", err)
	}
	pkg, rev, err := scope.FromBundle(b, abs, label, key, scope.Options{OnlyMeasured: true})
	if err != nil {
		_ = os.RemoveAll(abs)
		return nil, refuse("workflow.package", "", "%v", err)
	}

	// The reference run, step by step, as reference runs of the package: its executions ARE the
	// evidence of what the workflow produces on its test data.
	reg, err := pregistry.Open(cfg.PlanktonDir)
	if err != nil {
		return nil, refuse("kernel", "", "%v", err)
	}
	pub, err := loadPublic(strings.TrimSuffix(cfg.PlanktonKey, ".key") + ".pub")
	if err != nil {
		return nil, refuse("identity", "", "%v", err)
	}
	out := &WorkflowExtractResult{Dir: rel, Package: pkg.ID, Revision: rev.ID}
	for _, st := range prop.Steps {
		id, ok := st.Executions[run]
		if !ok {
			continue
		}
		env, ok := reg.Envelope(id)
		if !ok {
			return nil, refuse("kernel", "", "the execution %s of step %s is not in this registry", id, st.ID)
		}
		f, _ := reg.Foton(id)
		bytesOf := map[string][]byte{}
		for _, o := range f.Outputs {
			body, err := os.ReadFile(filepath.Join(cfg.RepoRoot, filepath.FromSlash(o.Path)))
			if err != nil {
				return nil, refuse("io", "", "the output %s of step %s: %v", o.Path, st.ID, err)
			}
			bytesOf[o.Path] = body
		}
		if _, err := rev.AddRun(env, pub, scope.RunReference, map[string]any{"step": st.ID}, bytesOf); err != nil {
			_ = os.RemoveAll(abs)
			return nil, refuse("workflow.package", "", "step %s: %v", st.ID, err)
		}
		out.References = append(out.References, id)
	}
	if out.Ray, err = rev.RayID(); err != nil {
		return nil, refuse("workflow.package", "", "%v", err)
	}
	if out.Seal, err = rev.Seal(); err != nil {
		return nil, refuse("workflow.package", "", "%v", err)
	}
	if _, err := rev.WriteView(); err != nil {
		return nil, refuse("workflow.package", "", "%v", err)
	}
	if probs := pkg.Verify(); len(probs) > 0 {
		return nil, refuse("workflow.package", "", "the package fails its own check: %s", probs[0])
	}
	for _, h := range in.Choice.Holes {
		out.Holes = append(out.Holes, h.Name)
	}
	for _, p := range in.Choice.Params {
		out.Params = append(out.Params, p.Name)
	}
	revn, err := persist(ctx, cfg, []string{rel}, "workflow: "+in.Choice.Name)
	if err != nil {
		return nil, refuse("store", "", "committing the package failed: %v", err)
	}
	out.CommitSHA = revn.ID
	return out, nil
}

// ---- list ------------------------------------------------------------------------------------------

// WorkflowListResult is the installed workflows, as the vocabulary's query "workflows" finds them.
type WorkflowListResult struct {
	Query     string              `json:"query"` // where the query came from
	Workflows []map[string]string `json:"workflows"`
}

// WorkflowList runs the vocabulary's "workflows" query over this repository's records.
func (c *Cockpit) WorkflowList(ctx context.Context) (*WorkflowListResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	rows, from, err := runQuery(ctx, cfg, "workflows")
	if err != nil {
		return nil, err
	}
	return &WorkflowListResult{Query: from, Workflows: rows}, nil
}

// ---- show ------------------------------------------------------------------------------------------

// WorkflowShowResult is one installed workflow, as a person choosing it needs to see it.
type WorkflowShowResult struct {
	Name       string              `json:"name"`
	Label      string              `json:"label"`
	Dir        string              `json:"dir"`
	Package    string              `json:"package"`
	Ray        string              `json:"ray"`
	Seal       string              `json:"seal"`
	Steps      []WorkflowStep      `json:"steps"`
	Holes      []ktonpkg.Hole      `json:"holes"`
	References map[string][]string `json:"references"` // step -> reference output files
}

// WorkflowStep is a step as shown.
type WorkflowStep struct {
	ID        string   `json:"id"`
	Label     string   `json:"label,omitempty"`
	Rationale string   `json:"rationale,omitempty"`
	Command   string   `json:"command"`
	Inputs    []string `json:"inputs"`
}

// WorkflowShow describes an installed workflow.
func (c *Cockpit) WorkflowShow(ctx context.Context, name string) (*WorkflowShowResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	in, b, err := installedWorkflow(cfg, name)
	if err != nil {
		return nil, err
	}
	out := &WorkflowShowResult{Name: in.Package.Name, Label: in.Revision.Label, Dir: in.Dir, Package: in.Package.ID,
		Seal: in.Seal.ID, Holes: b.Ray.Holes, References: map[string][]string{}}
	out.Ray, _ = in.Revision.RayID()
	order, _ := b.Ray.Order()
	for _, id := range order {
		s := b.Ray.Step(id)
		ws := WorkflowStep{ID: id, Label: s.Label, Rationale: s.Rationale,
			Command: ktonpkg.CommandLine(s, nil)}
		for _, i := range s.Inputs {
			from := i.From
			if i.Pinned() {
				from = "fixed"
			}
			ws.Inputs = append(ws.Inputs, i.Slot+" ← "+from)
		}
		out.Steps = append(out.Steps, ws)
	}
	refs, _ := b.List(ktonpkg.ReferenceDir)
	for _, f := range refs {
		base := path.Base(f)
		for _, id := range order {
			if strings.HasPrefix(base, id+"-") {
				out.References[id] = append(out.References[id], strings.TrimPrefix(base, id+"-"))
			}
		}
	}
	return out, nil
}

// ---- run -------------------------------------------------------------------------------------------

// WorkflowRunRequest performs an installed workflow.
type WorkflowRunRequest struct {
	Name     string            `json:"name"`
	Bindings map[string]string `json:"bindings,omitempty"`
	// Check runs the workflow on its test data and says, per step, whether this repository
	// reproduces the package's reference run (`reproduces`, in the installation's scope).
	Check bool `json:"check,omitempty"`
	// Dir is the working directory, repo-relative; work/<name> by default.
	Dir string `json:"dir,omitempty"`
}

// WorkflowRunResult reports a run, step by step.
type WorkflowRunResult struct {
	Name     string            `json:"name"`
	Dir      string            `json:"dir"`
	Bindings map[string]string `json:"bindings"`
	Steps    []WorkflowRunStep `json:"steps"`
}

// WorkflowRunStep is one step of a run.
type WorkflowRunStep struct {
	ID       string            `json:"id"`
	Cmd      string            `json:"cmd"`
	FotonID  string            `json:"fotonId"`
	Outputs  map[string]string `json:"outputs"`
	CoSigned bool              `json:"coSigned,omitempty"`
	// With check: the reference run of this step, whether this run is the same record, and the
	// reproduction level the cockpit computed.
	Reference  string `json:"reference,omitempty"`
	SameRecord bool   `json:"sameRecord,omitempty"`
	Level      string `json:"level,omitempty"`
	ClaimID    string `json:"claimId,omitempty"`
}

// WorkflowRun binds, stages and publishes the workflow's steps in order.
func (c *Cockpit) WorkflowRun(ctx context.Context, in WorkflowRunRequest) (*WorkflowRunResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	inst, b, err := installedWorkflow(cfg, in.Name)
	if err != nil {
		return nil, err
	}
	if in.Check && len(in.Bindings) > 0 {
		return nil, refuse("argument", "", "check runs the workflow on its own test data; bindings would make it a different run")
	}
	refs := map[string]string{}
	if in.Check {
		if refs, err = referenceRuns(inst.Revision); err != nil {
			return nil, refuse("workflow.package", "", "%v", err)
		}
	}
	work := in.Dir
	if work == "" && in.Check {
		// A check runs where the reference ran: paths are part of a record's identity, so only
		// there is the same work the same record, and this repository's run co-signs the
		// reference instead of standing next to it.
		work = referenceDir(cfg, refs)
	}
	if work == "" {
		work = path.Join("work", inst.Package.Name)
	}
	// Test data binds only a check. A run that is not a check and leaves a required hole open
	// does not start (URS-6): silently running on the package's test data would produce a result
	// that looks like the user's and is not.
	plan, err := b.Plan(work, ktonpkg.Bindings(in.Bindings), ktonpkg.PlanOptions{NoDefaults: !in.Check})
	if err != nil {
		if m, ok := err.(*ktonpkg.MissingError); ok {
			return nil, refuse("workflow.unbound", "", "%v — bind them with --bind NAME=VALUE, or run --check to run on the package's test data", m)
		}
		return nil, refuse("workflow.plan", "", "%v", err)
	}
	out := &WorkflowRunResult{Name: inst.Package.Name, Dir: work, Bindings: plan.Bindings}
	for _, st := range plan.Steps {
		for _, sg := range st.Stage {
			if err := copyRepoFile(cfg.RepoRoot, sg.From, sg.To); err != nil {
				return nil, refuse("io", "", "step %s: %v", st.ID, err)
			}
		}
		pub, err := c.Publish(ctx, PublishRequest{Cmd: st.Cmd, Inputs: st.Inputs, Outputs: st.Outputs})
		if err != nil {
			return nil, fmt.Errorf("step %s: %w", st.ID, err)
		}
		rs := WorkflowRunStep{ID: st.ID, Cmd: st.Cmd, FotonID: pub.FotonID, Outputs: pub.OutputHashes, CoSigned: pub.CoSigned}
		if in.Check {
			rs.Reference = refs[st.ID]
			rs.SameRecord = rs.Reference == pub.FotonID
			if rs.Reference != "" {
				lvl, claim, err := c.checkStep(ctx, cfg, inst, rs.Reference, pub)
				if err != nil {
					return nil, fmt.Errorf("step %s: %w", st.ID, err)
				}
				rs.Level, rs.ClaimID = lvl, claim
			}
		}
		out.Steps = append(out.Steps, rs)
	}
	return out, nil
}

// checkStep says `reproduces` about the reference run of a step: the cockpit computes the level
// itself by comparing outputs (SPEC §8.2), and the claim goes into the installation's scope.
func (c *Cockpit) checkStep(ctx context.Context, cfg *config.Config, inst *packages.Installed, ref string, pub *PublishResult) (string, string, error) {
	reg, err := pregistry.Open(cfg.PlanktonDir)
	if err != nil {
		return "", "", err
	}
	f, ok := reg.Foton(ref)
	if !ok {
		return "", "", fmt.Errorf("the reference run %s is not in this registry — was the package installed?", ref)
	}
	for _, o := range f.Outputs {
		for p := range pub.OutputHashes {
			if path.Base(p) != path.Base(o.Path) {
				continue
			}
			res, err := c.Say(ctx, SayRequest{Subject: ref, Template: "reproduces", SubjectOutputHash: o.Hash,
				ReproducedOutput: p, ReproducedFotonID: pub.FotonID,
				Scope: inst.Package.Name + "@" + inst.Revision.Label})
			if err != nil {
				return "", "", err
			}
			return res.Level, res.ClaimID, nil
		}
	}
	return "", "", fmt.Errorf("no output of this step matches the reference run's outputs")
}

// referenceDir is the working directory the reference run used: the parent of its step
// directories, read from an output path of a reference run (<dir>/<step>/<file>).
func referenceDir(cfg *config.Config, refs map[string]string) string {
	reg, err := pregistry.Open(cfg.PlanktonDir)
	if err != nil {
		return ""
	}
	for _, id := range refs {
		if f, ok := reg.Foton(id); ok && len(f.Outputs) > 0 {
			return path.Dir(path.Dir(f.Outputs[0].Path))
		}
	}
	return ""
}

// referenceRuns maps step -> reference run of a revision.
func referenceRuns(rev *scope.Revision) (map[string]string, error) {
	claims, err := rev.Claims()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, c := range claims {
		if c.Predicate != scope.PartOf {
			continue
		}
		v, _ := c.Object["value"].(map[string]any)
		if v["role"] == scope.RoleRun && v["kind"] == scope.RunReference {
			if step, ok := v["step"].(string); ok {
				out[step] = c.Subject
			}
		}
	}
	return out, nil
}

// installedWorkflow finds an installed package and opens its readable view as a workflow bundle,
// with paths relative to the repository so a plan's staging reads from inside it.
func installedWorkflow(cfg *config.Config, name string) (*packages.Installed, *ktonpkg.Bundle, error) {
	if name == "" {
		return nil, nil, refuse("argument", "", "which workflow? `cockpit workflow list` shows the installed ones")
	}
	in, err := packages.Find(cfg.RepoRoot, name)
	if err != nil {
		return nil, nil, refuse("workflow.unknown", "", "%v", err)
	}
	if prof, _ := in.Revision.Profile(); prof != scope.ProfileWorkflow {
		return nil, nil, refuse("workflow.unknown", "", "%s is a %s package, not a workflow", name, prof)
	}
	view := path.Join(in.Dir, scope.FilesDir, in.Revision.Label)
	if _, err := os.Stat(filepath.Join(cfg.RepoRoot, filepath.FromSlash(view), "kton")); err != nil {
		if _, err := in.Revision.WriteView(); err != nil {
			return nil, nil, refuse("workflow.package", "", "%v", err)
		}
	}
	b, err := ktonpkg.Open(filepath.Join(cfg.RepoRoot, filepath.FromSlash(view)))
	if err != nil {
		return nil, nil, refuse("workflow.package", "", "%v", err)
	}
	b.Dir = view
	if b.Ray == nil {
		return nil, nil, refuse("workflow.package", "", "%s carries no ray", name)
	}
	return in, b, nil
}

func copyRepoFile(root, from, to string) error {
	src, err := os.Open(filepath.Join(root, filepath.FromSlash(from)))
	if err != nil {
		return err
	}
	defer src.Close()
	dst := filepath.Join(root, filepath.FromSlash(to))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	w, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, src); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

func loadPrivate(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s is not a signing key", path)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func loadPublic(path string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%s is not a public key", path)
	}
	return ed25519.PublicKey(raw), nil
}

var _ = sort.Strings
