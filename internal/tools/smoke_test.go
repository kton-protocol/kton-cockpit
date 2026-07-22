package tools

import (
	"context"
	"os"
	"testing"
)

// These tests drive the real tool handlers against the actual, already-populated
// participant-alice-1 registry from the original live federation demo run — no fixtures to
// fabricate, and it exercises the anti-wrong-folder guard against a real git remote.
const realParticipantRepo = "/mnt/c/dev/planktonReproduce/alice/participant-alice-1"

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Skipf("repo not available in this environment: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestAsk_ProducerAgainstRealRegistry(t *testing.T) {
	chdir(t, realParticipantRepo)

	// sha256 hash of session-1/clean.csv, computed via `plankton hash` against this real repo.
	const cleanOutputHash = "sha256:a029b5fdd6f842f6a28a9d7aec27522295526da5de0af1cb791c5de4a200588a"

	result, out, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: cleanOutputHash})
	if err != nil {
		t.Fatalf("Ask returned Go error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Ask reported a tool error: %+v", result.Content)
	}
	if len(out.Records) == 0 {
		t.Fatalf("expected at least one record mentioning a producer foton, got none. raw=%s", out.Raw)
	}
	if len(out.Included) == 0 {
		t.Fatalf("expected at least one verified+included record (session-1's own key is in trust.tiers.self), got none.\nrecords=%+v\nraw=%s", out.Records, out.Raw)
	}
	t.Logf("producer query: %d records, %d included, filter=%q", len(out.Records), len(out.Included), out.FilterApplied)
}

func TestAsk_ReproductionsAgainstRealRegistry(t *testing.T) {
	chdir(t, realParticipantRepo)

	const cleanOutputHash = "sha256:a029b5fdd6f842f6a28a9d7aec27522295526da5de0af1cb791c5de4a200588a"

	_, out, err := Ask(context.Background(), nil, AskInput{Query: "reproductions", Ref: cleanOutputHash})
	if err != nil {
		t.Fatalf("Ask returned Go error: %v", err)
	}
	if out.Raw == "" {
		t.Fatal("expected non-empty raw reproductions output")
	}
	t.Logf("reproductions raw: %s", out.Raw)
}

func TestAsk_UnknownQueryIsRejected(t *testing.T) {
	chdir(t, realParticipantRepo)

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "delete-everything", Ref: "sha256:whatever"})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for an unknown query verb")
	}
}

func TestConfigGuard_RefusesOutsideAnyGitRepo(t *testing.T) {
	chdir(t, os.TempDir())

	result, _, err := Ask(context.Background(), nil, AskInput{Query: "producer", Ref: "sha256:deadbeef"})
	if err != nil {
		t.Fatalf("expected a tool-level error, not a Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected the anti-wrong-folder guard to refuse outside any git repository")
	}
}
