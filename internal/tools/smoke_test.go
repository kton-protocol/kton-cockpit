package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
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
	r.WriteConfig(t, raw)

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
	r.WriteConfig(t, raw)

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

// The property that makes an environment pin worth anything: it is COVERED, so the same command
// over the same inputs in a DIFFERENT pinned environment is a different foton, and a reproduction
// via it commits to re-executing there. Two repos, identical publishes, one with a pin.
func TestPublish_TheEnvironmentPinChangesTheFotonIdentity(t *testing.T) {
	const digest = "oci://ghcr.io/example/analysis@sha256:1111111111111111111111111111111111111111111111111111111111111111"

	unpinned := testrepo.New(t)
	unpinned.Use(t)
	a := publishOne(t, unpinned)
	if a.EnvRef != "" {
		t.Fatalf("no environment was configured, yet publish reported %q", a.EnvRef)
	}

	pinned := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Environment = config.Environment{EnvRef: digest}
	pinned.WriteConfig(t, raw)
	pinned.Use(t)
	b := publishOne(t, pinned)

	if b.EnvRef != digest {
		t.Fatalf("publish did not report the pinned environment: %q", b.EnvRef)
	}
	if a.FotonID == b.FotonID {
		t.Fatalf("the environment pin did not reach the foton: both runs produced %s, so --env-ref was not passed", a.FotonID)
	}
	// Same bytes either way — only the recorded environment differs.
	if a.OutputHashes["data/out.csv"] != b.OutputHashes["data/out.csv"] {
		t.Fatalf("the two runs produced different outputs, so the differing foton ids prove nothing")
	}
}

// A tag is a moving target: it names whatever it points at today, so a reproduction committing to
// "this environment" would commit to nothing. Since the value is covered, a wrong pin does not
// fail — it silently produces a foton pinning something other than what ran.
func TestConfig_RefusesAnOciEnvRefWithoutADigest(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Environment = config.Environment{EnvRef: "oci://ghcr.io/example/analysis:latest"}
	r.WriteConfig(t, raw)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("an oci:// env-ref without a digest was accepted; it pins no environment at all")
	}
	if !strings.Contains(errText(result), "digest") {
		t.Fatalf("the error does not explain the problem: %s", errText(result))
	}
}

func TestConfig_RefusesASpectrumThatIsNotAContentHash(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Environment = config.Environment{Spectrum: "our-standard-image"}
	r.WriteConfig(t, raw)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a non-hash env-spectrum was accepted")
	}
}

// plankton computes the ↻N count itself, over exactly the keys the cockpit hands it. So a tier
// filter has to be pushed down into --trust-keys, not applied to the records afterwards:
// previously every tier's keys went in and only the record LIST was filtered, so a query asking
// for one tier got a count spanning all of them while the response said trustTier=<that one>.
func TestAsk_ReproductionsCountIsScopedToTheRequestedTier(t *testing.T) {
	r := testrepo.New(t)

	// Two tiers, and the key that actually signs fotons is in "other" — so a query scoped to
	// "self" must find nothing, and one scoped to "other" must find the publisher.
	raw := testrepo.DefaultConfig()
	raw.Trust.Tiers = map[string][]string{
		"self":  {"registry/keys/" + testrepo.SessionID + "-claims.pub"},
		"other": {"registry/keys/" + testrepo.SessionID + ".pub"},
	}
	r.WriteConfig(t, raw)
	r.Use(t)
	pub := publishOne(t, r)
	hash := pub.OutputHashes["data/out.csv"]

	ask := func(tier string) AskOutput {
		t.Helper()
		in := AskInput{Query: "reproductions", Ref: hash}
		if tier != "" {
			in.Filter = &AskFilter{TrustTier: tier}
		}
		result, out, err := Ask(context.Background(), nil, in)
		if err != nil {
			t.Fatalf("Ask returned a Go error: %v", err)
		}
		if result.IsError {
			t.Fatalf("reproductions(tier=%q) failed: %s", tier, errText(result))
		}
		return out
	}

	if got := ask("other").VerifiedSigners; got != 1 {
		t.Errorf("tier \"other\" holds the signing key, so it must count 1 signer; got %d", got)
	}
	if got := ask("self").VerifiedSigners; got != 0 {
		t.Errorf("tier \"self\" does not hold the signing key, so the count must be 0, not a number "+
			"taken over every tier; got %d", got)
	}
	if got := ask("").VerifiedSigners; got != 1 {
		t.Errorf("unfiltered, every configured tier counts: expected 1, got %d", got)
	}
}

// The lineage queries return records, not text to be parsed by whoever reads the answer.
func TestAsk_ProducerReturnsAStructuredFotonRecord(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)

	result, out, err := Ask(context.Background(), nil, AskInput{
		Query: "producer", Ref: pub.OutputHashes["data/out.csv"],
	})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("producer query failed: %s", errText(result))
	}
	if len(out.Fotons) != 1 {
		t.Fatalf("expected exactly the one foton just published, got %+v", out.Fotons)
	}
	f := out.Fotons[0]
	if f.ID != pub.FotonID {
		t.Errorf("foton id: got %s, published %s", f.ID, pub.FotonID)
	}
	// publishOne records two inputs and one output.
	if f.Inputs != 2 || f.Outputs != 1 {
		t.Errorf("input/output counts not decoded: %+v", f)
	}
	if f.Kind == "" {
		t.Errorf("kind not decoded: %+v", f)
	}
}

// The execution config is refused at load time for the ways it can be quietly wrong. None of these
// need a container runtime: a configuration that cannot mean what it says should never get as far
// as starting one.
func TestConfig_RefusesExecutionSettingsThatCannotMeanWhatTheySay(t *testing.T) {
	cases := map[string]struct {
		mutate func(*config.Raw)
		expect string
	}{
		"image without a digest": {
			func(raw *config.Raw) { raw.Execution.Image = "oci://ghcr.io/example/analysis:latest" },
			"digest",
		},
		"image that is not an oci reference": {
			func(raw *config.Raw) { raw.Execution.Image = "ghcr.io/example/analysis@sha256:abc" },
			"oci://",
		},
		// Running in one environment while recording another is a false record waiting to be signed.
		"execution and environment disagreeing": {
			func(raw *config.Raw) {
				raw.Execution.Image = "oci://ghcr.io/example/a@sha256:1111111111111111111111111111111111111111111111111111111111111111"
				raw.Environment.EnvRef = "oci://ghcr.io/example/b@sha256:2222222222222222222222222222222222222222222222222222222222222222"
			},
			"disagree",
		},
		"network allowed with nothing to run": {
			func(raw *config.Raw) { raw.Execution.Network = true },
			"nothing runs",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := testrepo.New(t)
			raw := testrepo.DefaultConfig()
			tc.mutate(&raw)
			r.WriteConfig(t, raw)
			r.Use(t)

			result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
			if err != nil {
				t.Fatalf("expected a tool-level error, not a Go error: %v", err)
			}
			if !result.IsError {
				t.Fatal("the configuration was accepted")
			}
			if !strings.Contains(errText(result), tc.expect) {
				t.Fatalf("the error does not explain the problem (want %q): %s", tc.expect, errText(result))
			}
		})
	}
}

// The counterpart to the container tests: with no execution configured, publish records a command
// it did not run, and says so by leaving executedIn empty.
func TestPublish_WithoutExecutionConfiguredTheCommandIsOnlyRecorded(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	out := publishOne(t, r)

	if out.ExecutedIn != "" {
		t.Errorf("nothing was configured to run the command, yet publish reported executedIn=%q", out.ExecutedIn)
	}
	if out.Stdout != "" {
		t.Errorf("publish reported stdout for a command it never ran: %q", out.Stdout)
	}
}

// Git stage 1: the cockpit can be told not to write to git. The record is still signed and
// registered — what goes away is the locators, because the commit a permalink would pin does not
// exist, and pinning some other commit would point a peer at bytes that are not there.
func TestPublish_WithCommitsOffTheRecordIsStillMadeButCarriesNoLocators(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Git = config.Git{Commit: testrepo.Bool(false)}
	r.WriteConfig(t, raw)
	r.Use(t)

	before := r.HeadSHA(t)
	out := publishOne(t, r)

	if !strings.HasPrefix(out.FotonID, "sha256:") {
		t.Fatalf("the foton was not registered: %q", out.FotonID)
	}
	if r.HeadSHA(t) != before {
		t.Error("commits are off, yet publish created one")
	}
	if out.CommitSHA != "" {
		t.Errorf("publish reported a commit sha (%q) for a commit it did not make", out.CommitSHA)
	}
	if len(out.Permalinks) != 0 {
		t.Errorf("permalinks were built without a commit to pin them to: %+v", out.Permalinks)
	}
	if out.Committed || out.Pushed {
		t.Errorf("publish reported committed=%v pushed=%v", out.Committed, out.Pushed)
	}
}

// Commits on, push off: the permalinks are correct and will resolve once someone pushes, so they
// are reported — together with the fact that they do not resolve yet.
func TestPublish_WithPushOffItCommitsLocallyAndSaysSo(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Git = config.Git{Push: testrepo.Bool(false)}
	r.WriteConfig(t, raw)
	r.Use(t)

	remoteBefore := r.OriginSHA(t)
	out := publishOne(t, r)

	if out.CommitSHA == "" || !out.Committed {
		t.Fatal("publish did not commit")
	}
	if out.Pushed {
		t.Error("publish reported a push it was configured not to make")
	}
	if r.OriginSHA(t) != remoteBefore {
		t.Error("push is off, yet the remote moved")
	}
	if len(out.Permalinks) == 0 {
		t.Error("a local commit is still a real commit — its permalinks should be reported")
	}
}

// Turning commits off must not weaken the guard. It is the reason this project exists, and it
// checks which repo the cockpit is in, not whether it writes to it.
func TestConfigGuard_StillRefusesTheWrongRepoWithCommitsOff(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Git = config.Git{Commit: testrepo.Bool(false)}
	raw.Repo.Owner = "someone-else"
	r.WriteConfig(t, raw)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("the guard did not refuse a config bound to a different repo")
	}
}

// `commit: false` alone is a complete statement; writing `push: true` beside it states something
// that cannot happen.
func TestConfig_RefusesPushTrueWithCommitFalse(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Git = config.Git{Commit: testrepo.Bool(false), Push: testrepo.Bool(true)}
	r.WriteConfig(t, raw)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a config that pushes without committing was accepted")
	}
	if !strings.Contains(errText(result), "nothing to push") {
		t.Fatalf("the error does not explain the problem: %s", errText(result))
	}
}

// The environment is named per call, because only the caller knows it: a session works across many
// containers. It is a claim, exactly as the inputs, outputs and command already are.
func TestPublish_TheCallerNamesTheEnvironmentAndItReachesTheFoton(t *testing.T) {
	const digest = "oci://ghcr.io/example/analysis@sha256:3333333333333333333333333333333333333333333333333333333333333333"

	plain := testrepo.New(t)
	plain.Use(t)
	a := publishOne(t, plain)

	named := testrepo.New(t)
	named.Use(t)
	named.Write(t, "data/in.csv", "id,value\n1,42\n")
	named.Write(t, "data/analyse.py", "print('deterministic')\n")
	named.Write(t, "data/out.csv", "id,result\n1,84\n")
	result, b, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv", "data/analyse.py"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "python data/analyse.py data/in.csv > data/out.csv",
		EnvRef:  digest,
	})
	if err != nil {
		t.Fatalf("Publish returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("publish failed: %s", errText(result))
	}
	if b.EnvRef != digest {
		t.Fatalf("publish reported envRef %q, want %q", b.EnvRef, digest)
	}
	if a.FotonID == b.FotonID {
		t.Fatal("the supplied envRef did not reach the foton: identical ids for runs in different environments")
	}
}

func TestPublish_RefusesASuppliedEnvRefWithoutADigest(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	r.Write(t, "data/out.csv", "x\n")

	result, _, err := Publish(context.Background(), nil, PublishInput{
		Outputs: []string{"data/out.csv"},
		Cmd:     "true",
		EnvRef:  "oci://ghcr.io/example/analysis:latest",
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a tag was accepted as an environment pin; it names whatever it points at today")
	}
}

// A non-OCI reference is passed through: the substrate does not interpret the value, and a nix
// store path or a run-server id has no digest to require.
func TestPublish_AcceptsANonOciEnvironmentReference(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	r.Write(t, "data/out.csv", "x\n")

	const nixPath = "/nix/store/abc123-analysis-1.0"
	result, out, err := Publish(context.Background(), nil, PublishInput{
		Outputs: []string{"data/out.csv"},
		Cmd:     "true",
		EnvRef:  nixPath,
	})
	if err != nil || result.IsError {
		t.Fatalf("publish failed: err=%v %s", err, errText(result))
	}
	if out.EnvRef != nixPath {
		t.Fatalf("envRef: got %q, want %q", out.EnvRef, nixPath)
	}
}

// The cockpit takes a claim about the environment everywhere except where it knows better.
func TestPublish_WillNotRecordAnEnvironmentOtherThanTheOneItRanIn(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Execution = config.Execution{
		Image: "oci://ghcr.io/example/real@sha256:4444444444444444444444444444444444444444444444444444444444444444",
	}
	r.WriteConfig(t, raw)
	r.Use(t)
	r.Write(t, "data/out.csv", "x\n")

	result, _, err := Publish(context.Background(), nil, PublishInput{
		Outputs: []string{"data/out.csv"},
		Cmd:     "true",
		EnvRef:  "oci://ghcr.io/example/other@sha256:5555555555555555555555555555555555555555555555555555555555555555",
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("the cockpit accepted an environment other than the one it runs the command in")
	}
	if !strings.Contains(errText(result), "what it ran in") {
		t.Fatalf("the error does not explain the problem: %s", errText(result))
	}
}

// Git stage 2: no repository at all. The verbs work, the records are signed and registered, and
// what goes away is git — including the locators, which need an owner/repo and a commit.
func TestLocalMode_PublishAndSayWorkWithNoGitRepositoryAtAll(t *testing.T) {
	r := testrepo.NewLocal(t)
	r.Use(t)

	if _, err := os.Stat(filepath.Join(r.Root, ".git")); err == nil {
		t.Fatal("the fixture is supposed to have no git repository")
	}

	pub := publishOne(t, r)
	if !strings.HasPrefix(pub.FotonID, "sha256:") {
		t.Fatalf("the foton was not registered: %q", pub.FotonID)
	}
	if pub.CommitSHA != "" || len(pub.Permalinks) != 0 || pub.Committed || pub.Pushed {
		t.Errorf("git happened in a repo with no git: sha=%q permalinks=%d committed=%v pushed=%v",
			pub.CommitSHA, len(pub.Permalinks), pub.Committed, pub.Pushed)
	}

	claimID := sayWorkingOn(t, pub.FotonID)

	// And the record is queryable and verifies into a configured tier, which is the whole point:
	// none of signing, the registry or trust ever involved git.
	result, out, err := Ask(context.Background(), nil, AskInput{Query: "about", Ref: pub.FotonID})
	if err != nil || result.IsError {
		t.Fatalf("about query failed: err=%v %s", err, errText(result))
	}
	if len(out.Included) != 1 || out.Included[0] != claimID {
		t.Fatalf("the claim did not verify into a tier; records=%+v", out.Records)
	}
}

// The guard in local mode: the config must be where it says it is. This is the case it exists for —
// a directory copied next to its original, which is what the incident behind this project was.
func TestLocalMode_GuardRefusesAConfigThatWasCopiedElsewhere(t *testing.T) {
	r := testrepo.NewLocal(t)

	// A copy of the whole directory, carrying a config that still names the original.
	elsewhere := filepath.Join(t.TempDir(), "copy")
	if out, err := exec.Command("cp", "-a", r.Root, elsewhere).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v\n%s", err, out)
	}
	t.Setenv("COCKPIT_REPO_DIR", elsewhere)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("the cockpit acted against a copy of the directory its config was written for")
	}
	if !strings.Contains(errText(result), "location mismatch") {
		t.Fatalf("the error does not name the problem: %s", errText(result))
	}
}

// Committing to a repository that does not exist cannot happen, so saying so is refused rather
// than quietly ignored.
func TestLocalMode_RefusesGitCommitTrue(t *testing.T) {
	r := testrepo.NewLocal(t)
	raw := testrepo.DefaultConfig()
	raw.Repo = config.RepoRef{Mode: config.ModeLocal, Root: r.Root}
	raw.Git = config.Git{Commit: testrepo.Bool(true)}
	r.WriteConfig(t, raw)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a config that commits without a repository was accepted")
	}
}

// Local mode needs the anchor, or it is no guard at all: without repo.root the cockpit would act on
// whatever directory it was pointed at, which is the original incident exactly.
func TestLocalMode_RefusesAConfigWithNoDeclaredRoot(t *testing.T) {
	r := testrepo.NewLocal(t)
	raw := testrepo.DefaultConfig()
	raw.Repo = config.RepoRef{Mode: config.ModeLocal}
	r.WriteConfig(t, raw)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("local mode was accepted with no declared root")
	}
	if !strings.Contains(errText(result), "repo.root") {
		t.Fatalf("the error does not say what is missing: %s", errText(result))
	}
}

// In git mode the config must sit at the repository root, so that what it binds is unambiguous.
func TestGitMode_RefusesAConfigBelowTheRepositoryRoot(t *testing.T) {
	r := testrepo.New(t)
	sub := filepath.Join(r.Root, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(r.Root, "cockpit.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "cockpit.config.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCKPIT_REPO_DIR", sub)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a config below the repository root was accepted")
	}
}

// The integration-level companion to publish_denylist_test.go: it drives the real Publish() and
// confirms two things a unit test on the pure function cannot — that the denial happens inside
// Publish before anything else runs, and that NO git state changed. An error returned alongside a
// mutation that happened anyway would pass a check that only looked at the error.
func TestPublish_RefusesToCommitSigningKey(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	before := r.HeadSHA(t)

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Outputs: []string{"keys/session-1.key"},
		Cmd:     "cat keys/session-1.key",
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("publish committed a signing key: %+v", out)
	}
	if after := r.HeadSHA(t); after != before {
		t.Fatalf("HEAD moved (%s -> %s) although publish should have refused before touching git", before, after)
	}
}

// The regression test for the worse bypass: gitops built `git add` with no "--" separator, so
// outputs ["-f", "."] became `git add -f .` — a FLAG, not a path — force-adding every gitignored
// file including the keys, without "keys/" ever appearing in the request.
//
// The assertion that matters is the index, not HEAD: a partial "staged but never committed"
// failure would pass a HEAD-only check while the key sat in the index.
func TestPublish_RefusesLeadingDashOutput(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	before := r.HeadSHA(t)

	result, out, err := Publish(context.Background(), nil, PublishInput{
		Outputs: []string{"-f", "."},
		Cmd:     "echo pwned",
	})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("publish accepted a leading-dash output path: %+v", out)
	}
	if after := r.HeadSHA(t); after != before {
		t.Fatalf("HEAD moved (%s -> %s)", before, after)
	}
	if staged := stagedFiles(t, r.Root); staged != "" {
		t.Fatalf("expected nothing staged, found:\n%s", staged)
	}
}

func stagedFiles(t *testing.T, repoDir string) string {
	t.Helper()
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff --cached --name-only in %s: %v", repoDir, err)
	}
	return strings.TrimSpace(string(out))
}

// The config is looked for in two places and no others. An unbounded walk to the filesystem root
// would let a session in a directory with no config of its own bind to an ancestor's — including
// out of a git repository it is standing in — which is the incident this project exists for,
// reintroduced by a search.
func TestConfigGuard_DoesNotBindToAnAncestorsConfig(t *testing.T) {
	r := testrepo.New(t)

	// A directory beside the repo's root, inside the same parent, with no config of its own. The
	// parent holds one only because the fixture's repo dir is under it — which is exactly the shape
	// that used to be picked up.
	parent := filepath.Dir(r.Root)
	if err := os.WriteFile(filepath.Join(parent, "cockpit.config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(parent, "elsewhere")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCKPIT_REPO_DIR", stray)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("the cockpit bound to a config in a parent directory")
	}
	if !strings.Contains(errText(result), "never in an arbitrary parent") {
		t.Fatalf("the error does not say why: %s", errText(result))
	}
}

// A session in a SUBDIRECTORY of a real repo still resolves — the one step to the git root that
// bounding the search deliberately keeps.
func TestConfigGuard_ResolvesFromASubdirectoryOfTheRepo(t *testing.T) {
	r := testrepo.New(t)
	sub := filepath.Join(r.Root, "data", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COCKPIT_REPO_DIR", sub)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("Ask returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("a subdirectory of the repo should resolve to its root: %s", errText(result))
	}
}

// Naming local mode inside a repo that HAS an origin would trade the stronger check for the weaker
// one — the remote is an independent second source, the declared path is only the config agreeing
// with itself. A config that turns that off by naming a mode is the quiet downgrade the guard is for.
func TestLocalMode_RefusedInsideAGitRepoWithARemote(t *testing.T) {
	r := testrepo.New(t) // a real git repo with an origin
	raw := testrepo.DefaultConfig()
	raw.Repo = config.RepoRef{Mode: config.ModeLocal, Root: r.Root}
	r.WriteConfig(t, raw)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("local mode was accepted in a git repo that has an origin remote")
	}
	if !strings.Contains(errText(result), "drop the remote check") {
		t.Fatalf("the error does not explain the downgrade: %s", errText(result))
	}
}
