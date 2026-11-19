package cockpit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/gitops"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A say whose write meets git's lock still held after the wait is refused as store.busy, not
// store: busy can be tried again, broken cannot (userinterface's shell retries on busy; SPEC §12.1).
func TestSay_ALockedRepositoryIsRefusedAsBusy(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	subject := publishOne(t, r).FotonID
	defer gitops.SetLockWaitForTest(300 * time.Millisecond)()
	lock := filepath.Join(r.Root, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New(Start{}).Say(context.Background(), SayRequest{Subject: subject, Template: "working-on",
		Fields: map[string]string{"step": "checking"}})
	if ref := refusalOf(t, err); ref.Code != "store.busy" {
		t.Fatalf("want refusal store.busy, got %+v", ref)
	}
	os.Remove(lock)
	if _, err := New(Start{}).Say(context.Background(), SayRequest{Subject: subject, Template: "working-on",
		Fields: map[string]string{"step": "checking"}}); err != nil {
		t.Fatalf("the same say once the lock is gone: %v", err)
	}
}
