// Package backendtest is the conformance suite of cockpit/backend (ADR-008). A backend, including
// one in an extension outside this repository, runs it against itself:
//
//	func TestConformance(t *testing.T) { backendtest.Run(t, mybackend.Backend{}, myFixture{}) }
//
// It checks what the cockpit relies on and nothing about how a backend does it. The guard accepts
// the place its configuration belongs to and refuses another. A persisted file is durable. With no
// revision there is no locator. A locator gives back the bytes that were written.
package backendtest

import (
	"bytes"
	"context"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
)

// Fixture builds the repositories the suite runs against. Each call may build a fresh one.
type Fixture interface {
	// Bound returns a repository whose configuration belongs where it is: Bind must accept it.
	Bound(t *testing.T) backend.Repo
	// Elsewhere returns a configuration that does not belong where it is — pointed at another
	// repository, or copied to another place. Bind must refuse it.
	Elsewhere(t *testing.T) backend.Repo
	// Write puts a file into the repository, so Persist has something to make durable.
	Write(t *testing.T, r backend.Repo, path, content string)
	// Fetch retrieves the bytes a locator names. ok is false when the fixture cannot (offline);
	// the suite then checks only that a locator exists.
	Fetch(t *testing.T, r backend.Repo, uri string) (body []byte, ok bool)
}

// Run is the suite.
func Run(t *testing.T, b backend.Backend, f Fixture) {
	ctx := context.Background()

	t.Run("ValidateAcceptsTheBoundConfiguration", func(t *testing.T) {
		if err := b.Validate(f.Bound(t)); err != nil {
			t.Fatalf("Validate refused a configuration that belongs where it is: %v", err)
		}
	})

	t.Run("BindAcceptsWhereTheConfigurationBelongs", func(t *testing.T) {
		if err := b.Bind(ctx, f.Bound(t)); err != nil {
			t.Fatalf("Bind refused the place the configuration belongs to: %v", err)
		}
	})

	// The guard this project exists for (SPEC §5): a configuration in the wrong place is refused,
	// and nothing is written before it is.
	t.Run("BindRefusesWhereItDoesNot", func(t *testing.T) {
		if err := b.Bind(ctx, f.Elsewhere(t)); err == nil {
			t.Fatal("Bind accepted a configuration that does not belong where it is")
		}
	})

	t.Run("NoRevisionNoLocator", func(t *testing.T) {
		if uri, ok := b.Locate(f.Bound(t), backend.Revision{}, "any/file.txt"); ok {
			t.Fatalf("a locator without a revision points at bytes that are not there: %s", uri)
		}
	})

	t.Run("PersistedBytesComeBackThroughTheirLocator", func(t *testing.T) {
		r := f.Bound(t)
		const path, content = "work/conformance.txt", "dieselben Bytes\n"
		f.Write(t, r, path, content)
		rev, err := b.Persist(ctx, r, []string{path}, "conformance")
		if err != nil {
			t.Fatalf("Persist failed: %v", err)
		}
		if rev.Rejected {
			t.Fatalf("Persist was rejected: %s", rev.Reason)
		}
		uri, ok := b.Locate(r, rev, path)
		if rev.ID == "" {
			if ok {
				t.Fatalf("nothing was persisted, yet there is a locator: %s", uri)
			}
			return // a backend that persists nothing (local) has nothing to locate
		}
		if !ok {
			t.Log("this backend persists but gives no locators — allowed; not every store is fetchable")
			return
		}
		body, fetched := f.Fetch(t, r, uri)
		if !fetched {
			return
		}
		if !bytes.Equal(body, []byte(content)) {
			t.Fatalf("the locator %s gives other bytes than were written: %q", uri, body)
		}
	})
}
