//go:build live

// A real anchor in the PUBLIC Sigstore Rekor transparency log.
//
//	go test -tags live -run TestAnchorLive ./internal/tools/
//
// Behind a build tag, and the tag is not a formality: every run writes an entry to a public,
// append-only log that cannot be withdrawn. The kernel gates its own live Rekor test the same way.
// So the record it anchors is a throwaway — a fixture key generated for this test and discarded
// with the temp directory, over a file that says what it is — and what reaches the log is a
// signature over a hash, not the data.
//
// It exists because the stubbed tests cover this side of the network and nothing beyond it. Whether
// Rekor accepts what the cockpit submits, and whether what comes back is a real entry, only a real
// log can answer.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

func TestAnchorLive_TheEntryTheCockpitReportsIsInThePublicLog(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Anchor = config.Anchor{Enabled: true} // empty rekorUrl: the public log
	r.WriteConfig(t, raw)
	r.Use(t)

	r.Write(t, "data/in.csv", "this file exists only to be anchored by a test\n")
	r.Write(t, "data/out.csv", "kton-cockpit live anchor test\n")

	result, pub, err := Publish(context.Background(), nil, PublishInput{
		Inputs:  []string{"data/in.csv"},
		Outputs: []string{"data/out.csv"},
		Cmd:     "cockpit live anchor test — throwaway key, throwaway data",
	})
	if err != nil {
		t.Fatalf("Publish returned a Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("publish failed: %s", errText(result))
	}
	if pub.RekorUUID == "" || pub.RekorLogIndex == 0 {
		t.Fatalf("nothing was anchored: %+v", pub)
	}
	t.Logf("anchored: logIndex=%d uuid=%s", pub.RekorLogIndex, pub.RekorUUID)
	t.Logf("  https://search.sigstore.dev/?uuid=%s", pub.RekorUUID)

	// The cockpit says an entry exists. Ask the log, not the cockpit: fetching it back is the only
	// check here that the stubbed tests cannot make, and the whole reason this test is worth its
	// permanence.
	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("https://rekor.sigstore.dev/api/v1/log/entries/%s", pub.RekorUUID)
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("fetching the entry back from Rekor: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Rekor does not hold the entry the cockpit reported (%s): %s", pub.RekorUUID, resp.Status)
	}
	var entries map[string]struct {
		LogIndex int64 `json:"logIndex"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatalf("reading Rekor's answer: %v", err)
	}
	got, ok := entries[pub.RekorUUID]
	if !ok {
		t.Fatalf("Rekor returned no entry under %s", pub.RekorUUID)
	}
	if got.LogIndex != pub.RekorLogIndex {
		t.Fatalf("log index disagrees: the cockpit reported %d, Rekor holds %d", pub.RekorLogIndex, got.LogIndex)
	}

	// And the proof is beside the record, not merely reported back.
	_, ask, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: pub.OutputHashes["data/out.csv"]})
	if err != nil {
		t.Fatal(err)
	}
	if len(ask.Included) == 0 || !strings.HasPrefix(ask.Fotons[0].ID, "sha256:") {
		t.Fatalf("the anchored foton is not queryable: %+v", ask)
	}
}
