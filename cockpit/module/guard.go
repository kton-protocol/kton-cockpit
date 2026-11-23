package module

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// The guard door (ADR-019 section 4): before the core answers a request at a door (MCP, serve, the command
// line), it asks every registered guard. A guard is a module's; the core itself decides nothing about
// access (SPEC §13).

// Action is what a request does with its resources.
type Action string

const (
	Read  Action = "read"
	Write Action = "write"
	Sign  Action = "sign"
)

// Resource is one thing a request touches: a scope by its seed, a loaded file by its hash, the steps, the
// unscoped claims, or a signing key by its keyid. Classes are the data classes the core found for it (its
// classifiedAs claims, ADR-019 section 2); the core fills them, a guard does not look them up itself.
type Resource struct {
	Kind    string   `json:"kind"` // scope | loaded | steps | unscoped | key | tool
	ID      string   `json:"id,omitempty"`
	Classes []string `json:"classes,omitempty"`
}

// Session is the session a request acts as: its key and the claims verified about it. Nil for a request
// made without one (ADR-019 section 8: session.required decides whether that is allowed).
type Session struct {
	KeyID  string            `json:"keyid"`
	Claims map[string]string `json:"claims,omitempty"` // template -> claim id, verified by the core
}

// Request is what a guard is asked: who (Session), at which door, doing what, to which resources. The core
// derives the resources from what it will actually send or write, never from the request's text (G2).
type Request struct {
	Session   *Session   `json:"session,omitempty"`
	Door      string     `json:"door"` // mcp | serve | cli
	Tool      string     `json:"tool,omitempty"`
	Action    Action     `json:"action"`
	Resources []Resource `json:"resources"`
}

// Key names a resource in Decision.Denied: its kind and its ID.
func (r Resource) Key() string { return r.Kind + ":" + r.ID }

// Reason is why a resource is refused: the rule, and the attribute the session lacks.
type Reason struct {
	Rule    string `json:"rule"`
	Missing string `json:"missing,omitempty"`
}

// Decision is a guard's answer. Allow false refuses the request; Denied names the resources refused, by
// their Key (kind and ID: a scope's seed and a loaded file's hash are both sha256:…), so a read over many
// resources can leave those out and answer over the rest (section 8).
type Decision struct {
	Allow  bool              `json:"allow"`
	Denied map[string]Reason `json:"denied,omitempty"`
	Why    string            `json:"why,omitempty"`
}

// GuardTimeout is how long a guard may take before it counts as a refusal.
var GuardTimeout = 10 * time.Second

// Guarded says whether any registered module guards requests.
func Guarded() bool {
	mu.RLock()
	defer mu.RUnlock()
	for _, m := range modules {
		if m.Guard != nil {
			return true
		}
	}
	return false
}

// Decide asks every registered guard, in module name order, and joins their answers. It fails closed
// (kton-review G1 on ADR-019): a guard that panics, returns no answer within GuardTimeout, or whose context
// ends refuses, and the request is allowed only when every guard allows it; the resources any guard denies
// are all denied. Without a guard, every request is allowed and nothing changes.
func Decide(ctx context.Context, r Request) Decision {
	mu.RLock()
	var guards []Module
	for _, m := range modules {
		if m.Guard != nil {
			guards = append(guards, m)
		}
	}
	mu.RUnlock()
	sort.Slice(guards, func(i, j int) bool { return guards[i].Name < guards[j].Name })
	out := Decision{Allow: true}
	for _, m := range guards {
		d := ask(ctx, m, r)
		if !d.Allow {
			out.Allow = false
			if out.Why == "" {
				out.Why = d.Why
			}
		}
		for id, why := range d.Denied {
			if out.Denied == nil {
				out.Denied = map[string]Reason{}
			}
			if _, seen := out.Denied[id]; !seen {
				out.Denied[id] = why
			}
		}
	}
	return out
}

// ask runs one guard under its deadline; whatever is not an answer is a refusal.
func ask(ctx context.Context, m Module, r Request) Decision {
	ctx, cancel := context.WithTimeout(ctx, GuardTimeout)
	defer cancel()
	done := make(chan Decision, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- Decision{Why: fmt.Sprintf("the guard of module %q failed: %v", m.Name, p)}
			}
		}()
		done <- m.Guard(ctx, r)
	}()
	select {
	case d := <-done:
		if !d.Allow && d.Why == "" {
			d.Why = fmt.Sprintf("refused by the guard of module %q", m.Name)
		}
		return d
	case <-ctx.Done():
		return Decision{Why: fmt.Sprintf("the guard of module %q gave no answer (%v)", m.Name, ctx.Err())}
	}
}
