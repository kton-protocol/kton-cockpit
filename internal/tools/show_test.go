package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/show"
	"github.com/deathbychoco/claude-science-cockpit/internal/testrepo"
)

// showServer starts the data server over a fixture that already holds a foton and a claim.
func showServer(t *testing.T) (*httptest.Server, PublishOutput, string) {
	t.Helper()
	r := testrepo.New(t)
	r.Use(t)
	pub := publishOne(t, r)
	claimID := sayWorkingOn(t, pub.FotonID)

	srv, err := show.Start(context.Background(), r.Config(t))
	if err != nil {
		t.Fatalf("starting the record servers: %v", err)
	}
	t.Cleanup(srv.Stop)

	h, err := srv.Handler("")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, pub, claimID
}

func getJSON(t *testing.T, url string, into any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s: %s\n%s", url, resp.Status, b)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatalf("decoding %s: %v", url, err)
	}
}

// The union has to be the shape kton-web's reader accepts — its own test is
// `isRecord = (r) => !!(r && r.envelope && (r.fotonId || r.claimId))` — and it has to carry BOTH
// substrates. A union with the fotons and none of the claims is the documented way this goes wrong:
// the graph still draws, convincingly, and nothing errors.
func TestShow_TheUnionCarriesBothSubstratesInTheShapeAViewerReads(t *testing.T) {
	ts, pub, claimID := showServer(t)

	var union []struct {
		FotonID  string          `json:"fotonId"`
		ClaimID  string          `json:"claimId"`
		Envelope json.RawMessage `json:"envelope"`
	}
	getJSON(t, ts.URL+"/data/union.json", &union)

	var fotons, claims int
	var sawFoton, sawClaim bool
	for _, rec := range union {
		if len(rec.Envelope) == 0 {
			t.Errorf("a record has no envelope, so no viewer would count it: %+v", rec)
		}
		switch {
		case rec.FotonID != "":
			fotons++
			sawFoton = sawFoton || rec.FotonID == pub.FotonID
		case rec.ClaimID != "":
			claims++
			sawClaim = sawClaim || rec.ClaimID == claimID
		default:
			t.Errorf("a record carries neither a fotonId nor a claimId: %+v", rec)
		}
	}
	if fotons == 0 || claims == 0 {
		t.Fatalf("the union is missing a whole substrate: %d fotons, %d claims", fotons, claims)
	}
	if !sawFoton || !sawClaim {
		t.Fatalf("the union does not contain the records just written (foton=%v claim=%v)", sawFoton, sawClaim)
	}
}

// keys.json is what lets a viewer re-verify a signature instead of trusting a label, so the keyid it
// is filed under must be the one the signature actually carries.
func TestShow_KeysAreFiledUnderTheKeyidASignatureCarries(t *testing.T) {
	ts, _, _ := showServer(t)

	var union []struct {
		Envelope struct {
			Signatures []struct {
				KeyID string `json:"keyid"`
			} `json:"signatures"`
		} `json:"envelope"`
	}
	getJSON(t, ts.URL+"/data/union.json", &union)

	keys := map[string]string{}
	getJSON(t, ts.URL+"/data/keys.json", &keys)
	if len(keys) == 0 {
		t.Fatal("no keys served, so nothing in the viewer could be verified")
	}

	for _, rec := range union {
		for _, sig := range rec.Envelope.Signatures {
			hex, ok := keys[sig.KeyID]
			if !ok {
				t.Errorf("a record is signed by %s, which keys.json does not hold — the viewer could not verify it", sig.KeyID)
				continue
			}
			if len(hex) != 64 {
				t.Errorf("key %s is not a raw ed25519 public key: %q", sig.KeyID, hex)
			}
		}
	}
}

// The ring is this repo's configured trust tiers, not every pubkey lying in the registry: a viewer
// that re-verified against a wider ring than cockpit_ask uses would show as trusted exactly what ask
// correctly excludes.
func TestShow_TheRingIsTheConfiguredTrustTiers(t *testing.T) {
	ts, _, _ := showServer(t)

	names := map[string]string{}
	getJSON(t, ts.URL+"/data/names.json", &names)
	if len(names) != 2 {
		t.Fatalf("expected the two configured keys, got %+v", names)
	}
	for kid, n := range names {
		if !strings.Contains(n, "(self)") {
			t.Errorf("%s is named %q, which does not say which tier it came from", kid, n)
		}
	}
}

// The published union is a committed file, and it must not lag the registry it summarises: a
// viewer pointed at the repo online would otherwise show a graph missing the record whose publish
// wrote it.
func TestUnion_PublishedAlongsideTheRecordThatCausedIt(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Union = config.Union{Publish: true}
	r.WriteConfig(t, raw)
	r.Use(t)

	pub := publishOne(t, r)
	if !pub.UnionPublished {
		t.Fatal("publish did not report the union")
	}

	// Committed, not merely written.
	var tracked bool
	for _, f := range r.TrackedFiles(t) {
		if f == "docs/data/union.json" {
			tracked = true
		}
	}
	if !tracked {
		t.Fatalf("docs/data/union.json is not tracked; got %v", r.TrackedFiles(t))
	}

	b, err := os.ReadFile(filepath.Join(r.Root, "docs/data/union.json"))
	if err != nil {
		t.Fatal(err)
	}
	var union []struct {
		FotonID string `json:"fotonId"`
		ClaimID string `json:"claimId"`
	}
	if err := json.Unmarshal(b, &union); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, rec := range union {
		if rec.FotonID == pub.FotonID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the published union does not contain the foton whose publish wrote it (%s)", pub.FotonID)
	}

	// And a claim written afterwards lands in it too.
	claimID := sayWorkingOn(t, pub.FotonID)
	b, err = os.ReadFile(filepath.Join(r.Root, "docs/data/union.json"))
	if err != nil {
		t.Fatal(err)
	}
	union = union[:0]
	if err := json.Unmarshal(b, &union); err != nil {
		t.Fatal(err)
	}
	found = false
	for _, rec := range union {
		if rec.ClaimID == claimID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the published union does not contain the claim say just wrote (%s)", claimID)
	}
}

// A union that cannot be committed cannot be published.
func TestUnion_RefusedWhenTheRepoDoesNotCommit(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Union = config.Union{Publish: true}
	raw.Git = config.Git{Commit: testrepo.Bool(false)}
	r.WriteConfig(t, raw)
	r.Use(t)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: unknownHash})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a union was accepted in a repo that does not commit")
	}
}
