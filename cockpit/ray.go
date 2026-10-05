//go:build unreleased

package cockpit

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// A potential from executions that already ran (ADR-006).
//
// `ask {query: "ray"}` proposes and `publish {kind: "ray"}` writes what was chosen. Neither holds any
// rule of its own: grouping into runs and steps, the candidates, and the package all come from
// ktonpkg.Propose and ktonpkg.Extract, the same functions improvego calls on the other backend. What
// lives here is the selection, which is this cockpit's usual one: only records that verify against
// the configured trust tiers, by the same path `ask lineage` takes. There is also the adapter from
// a recorded foton to a backend-neutral execution.

// RayPublish is the ray half of a publish request.
type RayPublish struct {
	// Refs are the endpoints: the results of the runs to extract from, as hashes or files. One run
	// yields a potential whose holes can only be files. Only several runs show which place on a
	// command line varied, and so which parameters there are to choose from.
	Refs []string `json:"refs" jsonschema:"the endpoints of the runs to extract from: output hashes or repo-relative files"`
	// Choice is what a person chose from the proposal. Nothing becomes a hole or a parameter
	// unless it is named here.
	Choice ktonpkg.Choice `json:"choice" jsonschema:"what was chosen from the proposal of ask {query: ray}: name, reference run, holes, params, requires"`
	// Dir is where the package is written, repo-relative. It must not exist yet.
	Dir string `json:"dir" jsonschema:"repo-relative directory for the package; must not exist yet"`
}

// RayResult reports a published ray.
type RayResult struct {
	Dir         string   `json:"dir"`
	RayID       string   `json:"rayId"`
	BundleID    string   `json:"bundleId"`
	DerivedFrom []string `json:"derivedFrom"`
	// ClaimID is the signed prov:wasDerivedFrom claim from the ray to the executions it came from.
	ClaimID string `json:"claimId"`
}

// rayTemplate is the claim template a published ray is recorded under. It is not built in: the
// operator admits it in claims.allowedTemplates like any other, so the ceiling stays the
// configuration (SPEC §8.1).
const rayTemplate = "derived-from"

// rayExecutions selects the executions behind the endpoints and turns them into backend-neutral
// executions. It uses `ask lineage`'s own path, so a record that does not verify, or that the
// filter excludes, never reaches the proposal.
func rayExecutions(ctx context.Context, cfg *config.Config, r *binaries.Runner, refs []string,
	filter *AskFilter) ([]ktonpkg.Execution, map[string]string, []string, error) {
	if len(refs) == 0 {
		return nil, nil, nil, refuse("argument", "", "a ray needs at least one endpoint (refs)")
	}
	seen := map[string]bool{}
	var ids []string
	for _, ref := range refs {
		hash := ref
		if !strings.HasPrefix(ref, "sha256:") {
			h, _, err := hashLocalFile(ctx, cfg, r, ref)
			if err != nil {
				return nil, nil, nil, refuse("argument", "", "%v", err)
			}
			if h == "" {
				return nil, nil, nil, refuse("argument", "", "%s is neither a hash nor a file in this repo", ref)
			}
			hash = h
		}
		out, err := askLineage(ctx, cfg, r, AskRequest{Query: "lineage", Ref: hash}, filter)
		if err != nil {
			return nil, nil, nil, err
		}
		if len(out.Included) == 0 {
			return nil, nil, nil, refuse("ray.empty", "SPEC §9.7",
				"nothing verified leads to %s — a ray is extracted only from records that verify against a configured trust tier", ref)
		}
		for _, id := range out.Included {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)

	where := map[string]string{} // hash → repo-relative path, to fetch the bytes from
	var execs []ktonpkg.Execution
	for _, id := range ids {
		rec, err := r.FotonByID(ctx, id)
		if err != nil {
			return nil, nil, nil, refuse("kernel", "", "%v", err)
		}
		e, err := executionOf(rec, cfg.Raw.Execution.Entrypoint)
		if err != nil {
			return nil, nil, nil, refuse("ray.adapter", "SPEC §9.7", "%v", err)
		}
		for _, f := range append(append([]binaries.FotonFile{}, rec.Inputs...), rec.Outputs...) {
			where[f.Hash] = f.Path
		}
		execs = append(execs, e)
	}
	return execs, where, ids, nil
}

// executionOf reads a recorded foton as an execution. What it concludes are facts of the record,
// not guesses:
//
//   - A command `cd <dir> && …` ran in <dir>; otherwise it ran at the repository root.
//   - A slot is a path relative to where the command ran.
//   - The command file is the first word of the command line that is exactly an input slot, because
//     a program is named before its arguments.
//   - The step name is the last element of the working directory. It is a name, not a meaning.
//   - With an entrypoint configured (execution.entrypoint), the command line takes a package's form:
//     the image from the foton's envRef as the first line, and the entrypoint dropped, because the
//     tool supplies it. A command line that does not start with that entrypoint is refused rather
//     than turned into something else.
//
// A limit: the command line is split at spaces, so quoting is not understood.
func executionOf(rec *binaries.FotonDetail, entrypoint string) (ktonpkg.Execution, error) {
	dir, rest := "", rec.Cmd
	if strings.HasPrefix(rest, "cd ") {
		if d, after, ok := strings.Cut(strings.TrimPrefix(rest, "cd "), " && "); ok {
			dir, rest = d, after
		}
	}
	slot := func(p string) string {
		if dir != "" && strings.HasPrefix(p, dir+"/") {
			return strings.TrimPrefix(p, dir+"/")
		}
		return p
	}
	e := ktonpkg.Execution{ID: rec.ID, Command: strings.Fields(rest), Environment: rec.EnvRef}
	if dir != "" {
		e.Name = path.Base(dir)
	}
	inSlots := map[string]bool{}
	for _, f := range rec.Inputs {
		e.Inputs = append(e.Inputs, ktonpkg.ExecFile{Slot: slot(f.Path), Hash: f.Hash})
		inSlots[slot(f.Path)] = true
	}
	for _, f := range rec.Outputs {
		e.Outputs = append(e.Outputs, ktonpkg.ExecFile{Slot: slot(f.Path), Hash: f.Hash})
	}
	for _, tok := range e.Command {
		if inSlots[tok] {
			e.CommandFile = tok
			break
		}
	}
	if entrypoint != "" {
		if len(e.Command) == 0 || e.Command[0] != entrypoint {
			return e, fmt.Errorf("%s: the command line %q does not start with the tool's entrypoint %s", rec.ID, rest, entrypoint)
		}
		if rec.EnvRef == "" {
			return e, fmt.Errorf("%s: no envRef, so there is no image for the package's first line", rec.ID)
		}
		img := strings.TrimPrefix(strings.TrimPrefix(rec.EnvRef, "oci://"), "docker.io/")
		e.Command = append([]string{img}, e.Command[1:]...)
	}
	return e, nil
}

// askRay proposes a ray from the executions behind the endpoints. It decides nothing.
func askRay(ctx context.Context, cfg *config.Config, r *binaries.Runner, in AskRequest, filter *AskFilter) (*AskResult, error) {
	refs := in.Refs
	if len(refs) == 0 && in.Ref != "" {
		refs = []string{in.Ref}
	}
	execs, where, ids, err := rayExecutions(ctx, cfg, r, refs, filter)
	if err != nil {
		return nil, err
	}
	p, err := ktonpkg.Propose(execs)
	if err != nil {
		return nil, refuse("ray.propose", "SPEC §9.7", "%v", err)
	}
	endpoints := map[string]string{}
	for _, ru := range p.Runs {
		for _, e := range ru.Endpoints {
			endpoints[e] = where[e]
		}
	}
	return &AskResult{Query: in.Query, Ref: strings.Join(refs, ", "), FilterApplied: filter.describe(),
		Included: ids, Proposal: p, Executions: execs, Endpoints: endpoints,
		Raw: fmt.Sprintf("%d run(s), %d step(s), %d file candidate(s), %d parameter candidate(s), %d conflict(s)",
			len(p.Runs), len(p.Steps), len(p.Files), len(p.Params), len(p.Conflicts))}, nil
}

// publishRay writes the chosen potential as a package into the repository and records where it
// came from. The proposal is computed again here rather than taken from the caller: a proposal
// passed in could name candidates that do not exist, and a person may choose only what the
// executions actually offer.
func (c *Cockpit) publishRay(ctx context.Context, cfg *config.Config, in PublishRequest) (*PublishResult, error) {
	rp := in.Ray
	if rp == nil {
		return nil, refuse("argument", "", `publish {kind: "ray"} needs ray: {refs, choice, dir}`)
	}
	if !allowsTemplate(cfg, rayTemplate) {
		return nil, refuse("template.not-allowed", "SPEC §8.1",
			"a published ray is recorded as a %q claim, and this repo does not allow that template", rayTemplate)
	}
	if err := validatePublishPath(cfg, rp.Dir); err != nil {
		return nil, refuse("path.denied", "SPEC §7.3", "%v", err)
	}
	abs := filepath.Join(cfg.RepoRoot, filepath.FromSlash(rp.Dir))
	if _, err := os.Stat(abs); err == nil {
		return nil, refuse("ray.exists", "SPEC §7.6", "%s already exists — an extracted package is written new, never over an old one", rp.Dir)
	}

	r := binaries.New(cfg)
	execs, where, _, err := rayExecutions(ctx, cfg, r, rp.Refs, &AskFilter{})
	if err != nil {
		return nil, err
	}
	p, err := ktonpkg.Propose(execs)
	if err != nil {
		return nil, refuse("ray.propose", "SPEC §9.7", "%v", err)
	}
	fetch := func(hash string) ([]byte, error) {
		rel, ok := where[hash]
		if !ok {
			return nil, fmt.Errorf("no file with this hash among the selected records")
		}
		return os.ReadFile(filepath.Join(cfg.RepoRoot, filepath.FromSlash(rel)))
	}
	b, err := ktonpkg.Extract(p, execs, rp.Choice, fetch, abs)
	if err != nil {
		_ = os.RemoveAll(abs)
		return nil, refuse("ray.choice", "SPEC §7.6", "%v", err)
	}
	rayID, err := b.Ray.ID()
	if err != nil {
		return nil, refuse("ray.extract", "", "%v", err)
	}
	bundleID, err := b.ID()
	if err != nil {
		return nil, refuse("ray.extract", "", "%v", err)
	}
	derived := ktonpkg.DerivedFrom(p, rp.Choice.Reference)

	rev, err := persist(ctx, cfg, []string{rp.Dir}, "ray: "+rp.Choice.Name)
	if err != nil {
		return nil, refuse("store", "", "committing the package failed: %v", err)
	}
	pushRejected, sha := rev.Rejected, rev.ID

	claimID, err := r.Annotate(ctx, rayID, rayTemplate, map[string]string{
		"bundle":    bundleID,
		"sources":   strings.Join(derived, " "),
		"reference": rp.Choice.Reference,
		"dir":       rp.Dir,
	}, cfg.NektonKey, nil)
	if err != nil {
		return nil, refuse("kernel", "", "the package is written but recording where it came from failed: %v", err)
	}
	finalRev, err := persist(ctx, cfg, []string{cfg.Raw.Paths.NektonDir}, "claim: "+rayTemplate+" on "+rayID)
	if err != nil {
		return nil, refuse("store", "", "committing the claim failed: %v", err)
	}
	pushRejected = pushRejected || finalRev.Rejected
	finalSHA := finalRev.ID
	if finalSHA == "" {
		finalSHA = sha
	}
	return &PublishResult{
		CommitSHA:    finalSHA,
		Committed:    cfg.Raw.CommitEnabled(),
		Pushed:       cfg.Raw.PushEnabled() && !pushRejected,
		PushRejected: pushRejected,
		Ray: &RayResult{Dir: rp.Dir, RayID: rayID, BundleID: bundleID, DerivedFrom: derived,
			ClaimID: claimID},
	}, nil
}
