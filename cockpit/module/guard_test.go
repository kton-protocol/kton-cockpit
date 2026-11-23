package module

import (
	"context"
	"strings"
	"testing"
	"time"
)

func guard(t *testing.T, name string, g func(context.Context, Request) Decision) {
	t.Helper()
	if err := registerT(t, Module{Name: name, Guard: g}); err != nil {
		t.Fatal(err)
	}
}

var req = Request{Door: "mcp", Action: Read, Resources: []Resource{{Kind: "scope", ID: "sha256:a"}, {Kind: "scope", ID: "sha256:b"}}}

// Without a guard every request is allowed: nothing changes for a build without a guard module.
func TestDecide_NoGuardAllows(t *testing.T) {
	if Guarded() {
		t.Fatal("no guard is registered in this test")
	}
	if d := Decide(context.Background(), req); !d.Allow || len(d.Denied) != 0 {
		t.Errorf("no guard: want allowed, got %+v", d)
	}
}

// Every guard must allow, and the resources any guard denies are all denied (ADR-019 section 4).
func TestDecide_EveryGuardMustAllow(t *testing.T) {
	guard(t, "a-allows", func(context.Context, Request) Decision {
		return Decision{Allow: true, Denied: map[string]Reason{req.Resources[0].Key(): {Rule: "r1", Missing: "network none"}}}
	})
	guard(t, "b-allows", func(context.Context, Request) Decision { return Decision{Allow: true} })
	d := Decide(context.Background(), req)
	if !d.Allow || d.Denied["scope:sha256:a"].Rule != "r1" || len(d.Denied) != 1 {
		t.Errorf("both allow, one denies a resource: %+v", d)
	}
	guard(t, "c-refuses", func(context.Context, Request) Decision { return Decision{Why: "no session"} })
	if d := Decide(context.Background(), req); d.Allow || d.Why != "no session" {
		t.Errorf("one guard refuses: want refused with its reason, got %+v", d)
	}
}

// A guard that panics, does not answer in time, or answers on a context that ended refuses: fail closed
// (kton-review G1).
func TestDecide_FailsClosed(t *testing.T) {
	was := GuardTimeout
	GuardTimeout = 50 * time.Millisecond
	t.Cleanup(func() { GuardTimeout = was })
	for name, g := range map[string]func(context.Context, Request) Decision{
		"panics": func(context.Context, Request) Decision { panic("boom") },
		"hangs": func(ctx context.Context, _ Request) Decision {
			<-ctx.Done()
			time.Sleep(10 * time.Millisecond)
			return Decision{Allow: true}
		},
		"silent": func(context.Context, Request) Decision { return Decision{} },
	} {
		t.Run(name, func(t *testing.T) {
			guard(t, "g-"+name, g)
			d := Decide(context.Background(), req)
			if d.Allow || d.Why == "" || !strings.Contains(d.Why, "g-"+name) {
				t.Errorf("a guard that %s: want refused, naming it, got %+v", name, d)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	guard(t, "z-waits", func(ctx context.Context, _ Request) Decision {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		return Decision{Allow: true}
	})
	if d := Decide(ctx, req); d.Allow {
		t.Errorf("a request whose context ended: want refused, got %+v", d)
	}
}
