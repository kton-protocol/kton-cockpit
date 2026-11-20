package cockpit

import (
	"context"
	"sync"
	"testing"
)

// Claims said at the same time into one scope chain one after another: each takes the head the one
// before it wrote, so the scope keeps one head (qm-dev-provenance, 20.11.: five cockpit_say calls to
// one MCP process without waiting gave five claims on the same prev).
func TestScope_ConcurrentSaysChainOneAfterAnother(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	const n = 5
	var wg sync.WaitGroup
	errs := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, _, err := Say(context.Background(), nil, SayRequest{Subject: pub.FotonID, Template: "working-on",
				Scope: "review", Fields: map[string]string{"step": "analysis", "by-session": string(rune('a' + i))}})
			switch {
			case err != nil:
				errs[i] = err.Error()
			case res.IsError:
				errs[i] = errText(res)
			}
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != "" {
			t.Errorf("say %d: %s", i, e)
		}
	}
	if _, length, branched := r.ScopeHead(t, scope); branched || length != n {
		t.Errorf("concurrent says: want one chain of %d, got length %d, branched %v", n, length, branched)
	}
}
