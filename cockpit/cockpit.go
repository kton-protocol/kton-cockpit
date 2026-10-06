// Package cockpit is the kton cockpit as a library: exactly three verbs — Publish, Say and Ask —
// over a participant repository, with the anti-wrong-folder guard re-applied on every call.
//
// It is the one implementation. The command line (cmd/cockpit) and the MCP tool surface are thin
// transports over it and add nothing a caller of this package would not also get (SPEC §6); the
// same goes for bindings from other languages (ADR-005).
//
// A call either returns its result, or refuses with a *Refusal — the cockpit declining, with a
// stable code and a reason — or fails with any other error. A refusal is an answer, not a crash:
// the command line exits 2 for one and 1 for the other.
package cockpit

import (
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/material"
)

// Cockpit knows only where it starts — never the configuration. Every call re-resolves the
// configuration and re-checks the binding (SPEC §5), so nothing is cached across calls and a
// Cockpit can be kept for as long as its caller likes.
type Cockpit struct{ start Start }

// Start says where a Cockpit begins its search for cockpit.config.json.
type Start struct {
	// Dir is the starting directory; empty means the process's working directory. The
	// COCKPIT_REPO_DIR environment variable, when set, replaces it (SPEC §5.1).
	Dir string
}

// New returns a Cockpit that starts from s.
func New(s Start) *Cockpit { return &Cockpit{start: s} }

func (c *Cockpit) dir() string {
	if c == nil || c.start.Dir == "" {
		return "."
	}
	return c.start.Dir
}

// The records an answer carries are the kernel's own shapes. They are named here so a caller
// outside this module can name them too.
type (
	// ClaimAxis is one decoded claim, as the about and by queries return it.
	ClaimAxis = binaries.ClaimAxis
	// FotonRecord is one foton, as the lineage queries return it.
	FotonRecord = binaries.FotonRecord
	// FotonDetail is what one run did, as the record query returns it.
	FotonDetail = binaries.FotonDetail
	// MaterialReport is one item of evidence attached to a record, with whether it was evaluated.
	MaterialReport = material.Report
)
