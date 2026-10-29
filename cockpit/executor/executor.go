// Package executor is where a published command runs (ADR-011, ADR-008 E4): a port with a registry,
// like cockpit/backend. The core decides WHAT runs and records it; an executor only runs it.
//
// The reference implementation ships with the cockpit: executor/container (docker or podman, the
// pinned container of ADR-003). An extension brings its own the same way a backend does — improve
// brings its run server — and is chosen by execution.executor in cockpit.config.json.
//
// The snapshot diff that finds what a run wrote stays in the core. It is the core's own evidence of
// what changed, not something an executor reports about itself.
//
// Everything here uses only public types, so a module outside this repository can implement it.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Default is the executor used when the configuration names none.
const Default = "container"

// Step is one command to run, complete in itself: an executor needs nothing else from the cockpit.
type Step struct {
	// Root is the repository root, absolute. The command runs with it as its working directory.
	Root string
	// Image is what to run in, exactly as the foton will record it (oci://…@sha256:…).
	Image string
	// Engine selects the runtime within an executor (container: "docker" or "podman"). An executor
	// that has no such notion ignores it.
	Engine string
	// Network allows the command to reach the network. Off, it must not.
	Network bool
	// Hide are repository-relative directories the command must find empty, and whose changes must
	// not reach the repository: the cockpit's own trust base (keys, binaries, registries, .git).
	Hide []string
	// HideFiles are repository-relative files the command must find empty (cockpit.config.json).
	HideFiles []string
	// ReadOnly are host files or directories the command sees read-only at a path of its own, and
	// nothing it records: a licence a tool needs at run time (ADR-014). An executor that cannot
	// mount them refuses the step.
	ReadOnly []Mount
	// NoEntrypoint runs Cmd's shell directly instead of through the image's own entrypoint: a tool
	// shim runs its own command line, and an entrypoint that wraps the tool (improve's tool images
	// call nmfe themselves) would stand in front of it.
	NoEntrypoint bool
	// Cmd is the command line, run by a POSIX shell.
	Cmd string
	// Options is the configuration's execution.options block as written: the executor's own
	// settings, which the core does not read (a run server's address, whether it is offline).
	Options json.RawMessage
}

// Result is what one run produced.
type Result struct {
	Stdout string
	Stderr string
	// Image is the reference the run actually used. The core pins this same value into the foton,
	// so what ran and what is recorded cannot diverge.
	Image string
	// Removed are the repository-relative paths the command deleted, for an executor that does not
	// run in the repository itself and so cannot leave the deletion where the core's diff would see
	// it. Only what the command deleted belongs here, never what was not copied back.
	Removed []string
}

// A Validator checks an executor's options block when the configuration is loaded, so a bad block
// is refused before anything runs. Optional.
type Validator interface {
	Validate(options json.RawMessage) error
}

// Executor runs steps. A non-zero exit is an error: a run that failed produced nothing to record.
type Executor interface {
	Run(ctx context.Context, s Step) (Result, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Executor{}
)

// Register makes an executor available under a name. It is meant to be called from an init
// function, and it panics on a second registration of the same name.
func Register(name string, e Executor) {
	mu.Lock()
	defer mu.Unlock()
	if name == "" {
		panic("executor: Register with an empty name")
	}
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("executor: %q registered twice", name))
	}
	registry[name] = e
}

// Lookup returns the executor registered under name; the empty name is Default.
func Lookup(name string) (Executor, bool) {
	if name == "" {
		name = Default
	}
	mu.RLock()
	defer mu.RUnlock()
	e, ok := registry[name]
	return e, ok
}

// Names lists the registered executors, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Mount is a host path and where the command sees it.
type Mount struct {
	Host, At string
}
