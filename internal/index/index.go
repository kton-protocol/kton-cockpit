// Package index is the cockpit's read side as a port (ADR-012): everything ask, verify, material and
// scope read about records goes through a Reader, so how records are found can change without any
// of them changing.
//
// The only implementation today is "none": a binaries.Runner, which reads the registries through
// the substrate on every call and holds nothing between calls. It stays the reference every other
// implementation must answer identically to, and the fallback when one is unavailable. A derived
// index (memory, oxigraph) adds digesting — heads and deltas from the kernel's sync feed — and
// overrides the reads it can answer faster; anything it does not, it passes to a Runner.
//
// A Reader never decides trust. Tiers come from the calling repository's configuration at answer
// time (internal/verify), so the same record can be trusted in one repository and not in another.
package index

import (
	"context"
	"encoding/json"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"kton.dev/plankton/core"
)

// Reader is what the read side of the verbs asks. Records come from the union the configuration
// defines: the own registries and every configured federation source (SPEC §9.8).
type Reader interface {
	// Records is every record in the union, both kernels.
	Records(ctx context.Context) ([]binaries.Record, error)
	// EnvelopeFor is the signed envelope stored for a foton or claim id.
	EnvelopeFor(ctx context.Context, recordID string) (json.RawMessage, error)
	// Foton is the kernel's foton for an id; FotonByID is the same record as an answer shape.
	Foton(ctx context.Context, id string) (*core.Foton, error)
	FotonByID(ctx context.Context, id string) (*binaries.FotonDetail, error)

	// Lineage joins, by content hash (kton §11).
	Producer(ctx context.Context, hash string) (*binaries.LineageResult, error)
	Uses(ctx context.Context, hash string) (*binaries.LineageResult, error)
	Lineage(ctx context.Context, hash string) (*binaries.LineageResult, error)
	Reproductions(ctx context.Context, outputHash, tier string) (*binaries.ReproductionsResult, error)

	// Claims, by subject or by an axis.
	About(ctx context.Context, subject string) ([]binaries.ClaimAxis, error)
	By(ctx context.Context, axis binaries.ByAxis, value string) ([]binaries.ClaimAxis, error)

	// Verification material attached to a record (SPEC §8.1).
	MaterialForFoton(ctx context.Context, fotonID string) ([]binaries.StoredMaterial, error)
	MaterialForClaim(ctx context.Context, claimID string) ([]binaries.StoredMaterial, error)

	// Scopes, in the own nekton registry.
	Head(ctx context.Context, scopeID string) (*binaries.ScopeHead, error)
	ScopeChain(ctx context.Context, scopeID string) ([]binaries.ScopeClaim, error)
	Seed(ctx context.Context, scopeID string) (*binaries.ScopeSeed, error)

	// Two substrate computations every read path needs and no index changes: the keyid of a public
	// key, and the content hash of a file.
	KeyID(ctx context.Context, pubkeyPath string) (string, error)
	HashFile(ctx context.Context, absPath string) (string, error)
}

// The reference implementation is a Runner.
var _ Reader = (*binaries.Runner)(nil)

// Open returns the Reader for one call. Today that is always "none". The Runner it returns keeps
// the union it builds for the rest of the call (binaries.Runner), and nothing after it.
func Open(cfg *config.Config) Reader { return binaries.New(cfg) }
