package cockpit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/gitops"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A say whose claim is signed and stored before its commit meets git's lock is not refused: the
// claim exists, so it answers with it and pending store.busy, and the next write commits it. A
// refusal there made a retrying caller sign the same decision twice (kton-review B1 on #152). A
// write that meets the lock before anything is signed is still refused store.busy, and then nothing
// exists (SPEC §12.1).
func TestSay_ALockedRepositoryLeavesTheClaimPendingNotRefused(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	subject := publishOne(t, r).FotonID
	defer gitops.SetLockWaitForTest(300 * time.Millisecond)()
	lock := filepath.Join(r.Root, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	head := r.HeadSHA(t)
	about := func() int {
		t.Helper()
		_, ans, err := Ask(context.Background(), nil, AskRequest{Query: "about", Ref: subject})
		if err != nil {
			t.Fatal(err)
		}
		return len(ans.Claims)
	}

	out, err := New(Start{}).Say(context.Background(), SayRequest{Subject: subject, Template: "working-on",
		Fields: map[string]string{"step": "checking"}})
	if err != nil {
		t.Fatalf("a say whose commit meets the lock is refused: %v", err)
	}
	if out.ClaimID == "" || out.Pending == nil || out.Pending.Code != "store.busy" {
		t.Fatalf("want the claim with pending store.busy, got %+v", out)
	}
	if n := about(); n != 1 {
		t.Fatalf("after the pending say: %d claims, want exactly 1", n)
	}
	if r.HeadSHA(t) != head {
		t.Fatal("a pending say committed")
	}

	// Before anything is signed: publish's first commit meets the lock, and nothing exists after.
	r.Write(t, "data/busy.csv", "a\n1\n")
	if _, err := New(Start{}).Publish(context.Background(), PublishRequest{Cmd: "echo busy",
		Inputs: []string{"data/busy.csv"}, Outputs: []string{"data/busy.csv"}}); refusalOf(t, err).Code != "store.busy" {
		t.Fatalf("publish under the lock: want refusal store.busy, got %v", err)
	}

	// The next write, once the lock is gone, commits the pending claim with its own.
	os.Remove(lock)
	second, err := New(Start{}).Say(context.Background(), SayRequest{Subject: subject, Template: "working-on",
		Fields: map[string]string{"step": "reviewing"}})
	if err != nil || second.Pending != nil {
		t.Fatalf("the next say: %+v %v", second, err)
	}
	if n := about(); n != 2 {
		t.Fatalf("after the next say: %d claims, want 2 (no duplicate of the pending one)", n)
	}
	status, _ := exec.Command("git", "-C", r.Root, "status", "--porcelain", "--", ".kton/nekton").Output()
	if strings.TrimSpace(string(status)) != "" {
		t.Fatalf("the pending claim was not committed by the next write:\n%s", status)
	}
}
