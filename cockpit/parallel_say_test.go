package cockpit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// Several say processes on one working tree each commit their claim exactly once and succeed:
// none of them fails on git's index.lock while another commits (coordination's lanes/, where every
// lane says into one registry). Run as separate processes, so the lock between processes is
// what is tested, not only the one inside this one.
func TestSay_ParallelProcessesEachCommitTheirClaim(t *testing.T) {
	if os.Getenv("COCKPIT_TEST_SAY_CHILD") != "" {
		t.Skip("the child half runs in TestSay_ParallelChild")
	}
	r := testrepo.New(t)
	r.Use(t)
	subject := publishOne(t, r).FotonID
	// One claim first, so the claim registry exists, as it does in lanes/: the kernel creating a
	// registry is not safe for parallel writers (its .format marker can be read half-written).
	first := sayInScope(t, subject, "")

	const n = 6
	var wg sync.WaitGroup
	outs, errs := make([]string, n), make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestSay_ParallelChild$", "-test.count=1")
			cmd.Env = append(os.Environ(), "COCKPIT_TEST_SAY_CHILD="+subject, fmt.Sprintf("COCKPIT_TEST_SAY_STEP=step-%d", i))
			b, err := cmd.CombinedOutput()
			outs[i], errs[i] = string(b), err
		}()
	}
	wg.Wait()

	ids := map[string]bool{}
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("say %d failed: %v\n%s", i, errs[i], outs[i])
		}
		for _, l := range strings.Split(outs[i], "\n") {
			if id, ok := strings.CutPrefix(l, "claim "); ok {
				ids[strings.TrimSpace(id)] = true
			}
		}
	}
	delete(ids, first)
	if len(ids) != n {
		t.Fatalf("want %d distinct claims, got %d: %v", n, len(ids), ids)
	}
	if st, err := exec.Command("git", "-C", r.Root, "status", "--porcelain").CombinedOutput(); err != nil || strings.TrimSpace(string(st)) != "" {
		t.Fatalf("every claim committed: want a clean tree, got %q %v", st, err)
	}
	_, out, err := Ask(context.Background(), nil, AskRequest{Query: "about", Ref: subject})
	if err != nil {
		t.Fatal(err)
	}
	for id := range ids {
		if !strings.Contains(strings.Join(out.Included, " "), id) {
			t.Errorf("claim %s is not registered", id)
		}
	}
}

// TestSay_ParallelChild is one say of TestSay_ParallelProcessesEachCommitTheirClaim, in a process
// of its own; on its own it does nothing.
func TestSay_ParallelChild(t *testing.T) {
	subject := os.Getenv("COCKPIT_TEST_SAY_CHILD")
	if subject == "" {
		t.Skip("run by TestSay_ParallelProcessesEachCommitTheirClaim")
	}
	res, out, err := Say(context.Background(), nil, SayRequest{Subject: subject, Template: "working-on",
		Fields: map[string]string{"step": os.Getenv("COCKPIT_TEST_SAY_STEP"), "by-session": testrepo.SessionID}})
	if err != nil || res.IsError {
		t.Fatalf("say: %v %s", err, errText(res))
	}
	fmt.Printf("claim %s\n", out.ClaimID)
}
