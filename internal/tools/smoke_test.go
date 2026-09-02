package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/testrepo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// These drive the real tool handlers against a real participant repo with a real registry — one
// built from nothing by internal/testrepo on every run, so there is no hand-made clone to go
// stale. Records are genuinely signed by keys the real binaries generated, and commits genuinely
// push (to a local bare remote), so the assertions below are about what actually happened rather
// than about a fixture's shape.
//
// The previous version of this file pointed at a local clone by absolute path. When that path
// disappeared, every test here degraded to a skip and the suite went on reporting success while
// exercising none of this code.

// chdir changes to dir for the duration of the test. Only the guard tests need it; everything
// else points the cockpit at its repo with COCKPIT_REPO_DIR and leaves the process cwd alone.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

const unknownHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

// errText renders a tool result's content as text. Printing []mcp.Content with %+v yields a slice
// of pointers, which makes every failure message useless exactly when it is needed.
func errText(result *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// publishOne runs a real publish of one input and one output through the real handler, and
// returns its result. It fails the test if the publish itself failed.
func publishOne(t *testing.T, r *testrepo.Repo) PublishOutput {
	t.Helper()
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	r.Write(t, "data/analyse.py", "print('deterministic')\n")
	r.Write(t, "data/out.csv", "id,result\n1,84\n")

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv", "data/analyse.py"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "python data/analyse.py data/in.csv > data/out.csv",
	})
	if err != nil {
		t.Fatalf("Publish returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("publish failed: %+v", errText(result))
	}
	return out
}

func TestFixture_BuildsARepoThePassesTheRealGuard(t *testing.T) {
	r := testrepo.New(t)
	cfg := r.Config(t)

	if cfg.Raw.Repo.Owner != testrepo.Owner || cfg.Raw.Repo.Name != testrepo.Name {
		t.Fatalf("config did not round-trip: %+v", cfg.Raw.Repo)
	}
	for _, p := range []string{cfg.PlanktonDir, cfg.NektonDir, cfg.TemplatesDir, cfg.PlanktonKey, cfg.NektonKey} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("fixture is missing %s: %v", p, err)
		}
	}
}

func TestPublish_RecordsASignedFotonAndPushesIt(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)

	before := r.HeadSHA(t)
	out := publishOne(t, r)

	if !strings.HasPrefix(out.FotonID, "sha256:") {
		t.Fatalf("expected a sha256 foton id, got %q", out.FotonID)
	}
	if h := out.OutputHashes["data/out.csv"]; !strings.HasPrefix(h, "sha256:") {
		t.Fatalf("expected an output hash for data/out.csv, got %q", h)
	}
	if out.CommitSHA == before {
		t.Fatal("publish did not create a commit")
	}
	// The push is not incidental: without it the permalinks publish hands back point at bytes no
	// peer can fetch. Asserting against the bare remote is the only way to know it really ran.
	// Publish commits twice (data files, then the registry) and pins permalinks to the first, so
	// the reported sha must be CONTAINED in the remote rather than equal to its tip — and the tip
	// itself must match local HEAD, i.e. the registry commit was pushed too.
	if !r.OriginContains(t, out.CommitSHA) {
		t.Fatalf("publish did not push: remote main does not contain %s", out.CommitSHA)
	}
	if got, want := r.OriginSHA(t), r.HeadSHA(t); got != want {
		t.Fatalf("the registry commit was not pushed: remote is at %s, local HEAD is %s", got, want)
	}
	want := "https://raw.githubusercontent.com/" + testrepo.Owner + "/" + testrepo.Name + "/" + out.CommitSHA + "/data/out.csv"
	if got := out.Permalinks["data/out.csv"]; got != want {
		t.Fatalf("permalink not pinned to the publish commit:\n got %s\nwant %s", got, want)
	}
}

// The test the old suite could not have: that a query actually FINDS the thing that was just
// published, and that it lands in Included — verified against a configured trust tier, resolved
// from the verifying key. A query that finds nothing would have passed every assertion the
// previous producer test made.
func TestAsk_ProducerFindsTheJustPublishedFotonAndVerifiesIt(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: pub.OutputHashes["data/out.csv"]})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("producer query failed: %+v", errText(result))
	}
	if len(out.Included) == 0 {
		t.Fatalf("producer found nothing for a hash just published; records=%+v raw=%q", out.Records, out.Raw)
	}
	var found bool
	for _, rec := range out.Records {
		if rec.ID == pub.FotonID {
			found = true
			if !rec.Verified || rec.Tier != "self" {
				t.Fatalf("the published foton verified as %+v; expected verified in tier \"self\"", rec)
			}
		}
	}
	if !found {
		t.Fatalf("producer did not return the foton publish reported (%s); got %+v", pub.FotonID, out.Records)
	}
}

func TestAsk_ProducerOnUnknownHashIncludesNothing(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("a plain not-found query must not be a tool error: %+v", errText(result))
	}
	if len(out.Included) != 0 {
		t.Fatalf("expected nothing included for an unknown hash, got %+v", out.Included)
	}
}

// Against the 0.1 binaries this repo used to vendor, `reproductions --trust-keys` did not exist
// and the cockpit had to fail loudly rather than hand back a forgeable, self-declared count. The
// 0.2 kernel supports the flag, so the same query is now expected to succeed — and to report the
// one verified producer that actually exists.
func TestAsk_ReproductionsCountsTheVerifiedProducer(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "reproductions", Ref: pub.OutputHashes["data/out.csv"]})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("reproductions failed against a --trust-keys-capable binary: %+v", errText(result))
	}
	if !strings.Contains(out.Raw, "1") {
		t.Fatalf("expected a verified count of 1 for a single publisher, raw=%q", out.Raw)
	}
	t.Logf("reproductions raw: %s", out.Raw)
}

func TestAsk_UnknownQueryIsRejected(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "delete-everything", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for an unknown query verb")
	}
}

func TestSay_RecordsAClaimAndConfirmsItIsQueryable(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)

	result, out, err := Say(context.Background(), nil, SayInput{
		Subject:  pub.FotonID,
		Template: "working-on",
		Fields:   map[string]string{"step": "analysis", "by-session": testrepo.SessionID},
	})
	if err != nil {
		t.Fatalf("Say returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("say failed: %+v", errText(result))
	}
	if !strings.HasPrefix(out.ClaimID, "sha256:") {
		t.Fatalf("expected a sha256 claim id, got %q", out.ClaimID)
	}
	// The confirmation is `nekton about <subject>` run after the write — if the claim did not
	// really register, this is where it shows.
	if !strings.Contains(out.Confirmation, out.ClaimID) {
		t.Fatalf("the claim does not appear in its own confirmation query:\nclaim=%s\nabout=%s", out.ClaimID, out.Confirmation)
	}

	ask, askOut, err := Ask(context.Background(), nil, AskInput{Query: "about", Ref: pub.FotonID})
	if err != nil || ask.IsError {
		t.Fatalf("about query failed: err=%v result=%+v", err, errText(ask))
	}
	var included bool
	for _, id := range askOut.Included {
		if id == out.ClaimID {
			included = true
		}
	}
	if !included {
		t.Fatalf("the claim did not verify into a configured trust tier; records=%+v", askOut.Records)
	}
}

func TestSay_RefusesATemplateOutsideTheConfiguredCeiling(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)

	result, _, err := Say(context.Background(), nil, SayInput{Subject: unknownHash, Template: "gxp/review"})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("say accepted a template that is not in allowedTemplates")
	}
}

// The cockpit runs the reproduction precondition itself rather than believing a level Claude
// supplies. Outputs that do not match must not produce a claim at all.
func TestSay_ReproducesRefusesWhenTheOutputsDoNotMatch(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	r.Write(t, "data/different.csv", "id,result\n1,999\n")

	result, _, err := Say(context.Background(), nil, SayInput{
		Subject:           pub.FotonID,
		Template:          "reproduces",
		SubjectOutputHash: pub.OutputHashes["data/out.csv"],
		ReproducedOutput:  "data/different.csv",
		ReproducedFotonID: pub.FotonID,
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("say recorded a reproduces claim for outputs that do not reproduce")
	}
}

func TestSay_ReproducesRecordsL0ForIdenticalBytes(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	r.Write(t, "data/rerun.csv", "id,result\n1,84\n") // byte-identical to data/out.csv

	result, out, err := Say(context.Background(), nil, SayInput{
		Subject:           pub.FotonID,
		Template:          "reproduces",
		SubjectOutputHash: pub.OutputHashes["data/out.csv"],
		ReproducedOutput:  "data/rerun.csv",
		ReproducedFotonID: pub.FotonID,
	})
	if err != nil {
		t.Fatalf("Say returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("say refused a genuine byte-identical reproduction: %+v", errText(result))
	}
	if out.Level != "L0" {
		t.Fatalf("byte-identical outputs must be L0, got %q", out.Level)
	}
}

func TestConfigGuard_RefusesOutsideAnyGitRepo(t *testing.T) {
	chdir(t, os.TempDir())

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected the anti-wrong-folder guard to refuse outside any git repository")
	}
}

// The guard's actual job, which nothing exercised before: a config bound to one repo, sitting in
// a different one, must refuse — not act against the wrong repo.
func TestConfigGuard_RefusesWhenTheConfigNamesADifferentRepo(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)

	raw := testrepo.DefaultConfig()
	raw.Repo.Owner = "someone-else"
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Root, "cockpit.config.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("the guard did not refuse a config bound to a different repo")
	}
}

func TestConfigGuard_CockpitRepoDirEnvOverridesCwd(t *testing.T) {
	r := testrepo.New(t)
	chdir(t, os.TempDir()) // cwd is deliberately NOT the participant repo
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected COCKPIT_REPO_DIR to resolve against the fixture repo, got: %+v", errText(result))
	}
}

// sayWorkingOn records a working-on claim on the given subject and returns its id.
func sayWorkingOn(t *testing.T, subject string) string {
	t.Helper()
	result, out, err := Say(context.Background(), nil, SayInput{
		Subject:  subject,
		Template: "working-on",
		Fields:   map[string]string{"step": "analysis", "by-session": testrepo.SessionID},
	})
	if err != nil {
		t.Fatalf("Say returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("say failed: %+v", errText(result))
	}
	return out.ClaimID
}

// The point of reading nekton's --json mode rather than its prose: the prose carries the claim's
// id, predicate and declared signer, and nothing else. What the claim SAYS — the object, i.e. the
// values the template's fields were filled with — has no place in it at all.
func TestAsk_AboutReturnsWhatTheClaimSaysNotJustThatItExists(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	claimID := sayWorkingOn(t, pub.FotonID)

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "about", Ref: pub.FotonID})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("about query failed: %+v", errText(result))
	}
	if len(out.Claims) != 1 {
		t.Fatalf("expected exactly the one claim just recorded, got %d: %+v", len(out.Claims), out.Claims)
	}
	c := out.Claims[0]
	if c.ID != claimID {
		t.Fatalf("claim id mismatch: got %s, recorded %s", c.ID, claimID)
	}
	if c.Predicate != "https://kton.dev/v/working-on" {
		t.Fatalf("predicate not decoded: %q", c.Predicate)
	}
	if c.Subject != pub.FotonID {
		t.Fatalf("subject not decoded: got %q, want %q", c.Subject, pub.FotonID)
	}
	if c.When == "" || c.DeclaredBy == "" || len(c.SignatureKeyIDs) == 0 {
		t.Fatalf("envelope fields not decoded: %+v", c)
	}
	obj, ok := c.Object.(map[string]any)
	if !ok {
		t.Fatalf("object not decoded as a map: %#v", c.Object)
	}
	if obj["step"] != "analysis" || obj["by-session"] != testrepo.SessionID {
		t.Fatalf("the claim's own field values did not survive decoding: %+v", obj)
	}
}

// `ask` has advertised a "by" query in its schema since the beginning, but the cockpit called
// `nekton by <value>` with the axis omitted — a form every nekton back to 0.1 rejects with its
// usage line. The query could never return an answer. This is the first test that runs it.
func TestAsk_ByPredicateFindsTheClaim(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	claimID := sayWorkingOn(t, pub.FotonID)

	result, out, err := Ask(context.Background(), nil, AskInput{
		Query: "by",
		Axis:  "predicate",
		Ref:   "https://kton.dev/v/working-on",
	})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("by/predicate query failed: %+v", errText(result))
	}
	if len(out.Included) != 1 || out.Included[0] != claimID {
		t.Fatalf("by/predicate did not find the claim; included=%+v claims=%+v", out.Included, out.Claims)
	}
}

func TestAsk_BySignerFindsTheClaim(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	claimID := sayWorkingOn(t, pub.FotonID)

	// The signing keyid, taken from the claim's own envelope rather than recomputed.
	_, about, err := Ask(context.Background(), nil, AskInput{Query: "about", Ref: pub.FotonID})
	if err != nil || len(about.Claims) == 0 {
		t.Fatalf("could not read back the claim to learn its keyid: err=%v claims=%+v", err, about.Claims)
	}
	keyid := about.Claims[0].SignatureKeyIDs[0]

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "by", Axis: "signer", Ref: keyid})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("by/signer query failed: %+v", errText(result))
	}
	if len(out.Included) != 1 || out.Included[0] != claimID {
		t.Fatalf("by/signer did not find the claim; included=%+v", out.Included)
	}
}

func TestAsk_ByWithoutAnAxisIsRejected(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "by", Ref: "https://kton.dev/v/working-on"})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected `by` without an axis to be rejected, since nekton has no combined search")
	}
}

func TestAsk_ByWithAnUnknownAxisIsRejected(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "by", Axis: "everything", Ref: "x"})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected an unknown `by` axis to be rejected before it reaches nekton")
	}
}

// A claim outside every configured trust tier must not have its content assembled into the
// answer at all — not merely be flagged. The claims path never renders an excluded claim, so
// this checks the stronger property that Raw and Claims stay empty while Records still accounts
// for what was found.
func TestAsk_AboutExcludesAClaimThatVerifiesAgainstNoConfiguredTier(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	claimID := sayWorkingOn(t, pub.FotonID)

	// Empty the trust tiers: the claim is unchanged and still signed, but nothing in this repo's
	// config can verify it any more.
	raw := testrepo.DefaultConfig()
	raw.Trust.Tiers = map[string][]string{"self": {}}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Root, "cockpit.config.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "about", Ref: pub.FotonID})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("about query failed: %+v", errText(result))
	}
	if len(out.Claims) != 0 || out.Raw != "" {
		t.Fatalf("an unverifiable claim's content reached the answer: claims=%+v raw=%q", out.Claims, out.Raw)
	}
	if len(out.Excluded) != 1 || out.Excluded[0] != claimID {
		t.Fatalf("the claim was not accounted for as excluded: %+v", out.Excluded)
	}
}

// The private signing keys must never be committed, and the public halves must be — a peer
// cannot verify this participant's signatures without them. Both halves are generated side by
// side in keys/, so this is exactly the kind of thing that goes wrong quietly.
func TestFixture_CommitsThePublicKeysAndNeverThePrivateOnes(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	publishOne(t, r) // a publish commits more paths; the rule must still hold afterwards

	var sawPub bool
	for _, f := range r.TrackedFiles(t) {
		if strings.HasSuffix(f, ".key") {
			t.Errorf("a private signing key is tracked by git: %s", f)
		}
		if f == "registry/keys/"+testrepo.SessionID+".pub" {
			sawPub = true
		}
	}
	if !sawPub {
		t.Errorf("the published public key registry/keys/%s.pub is not tracked; a peer could not verify this participant", testrepo.SessionID)
	}
}
