package binaries

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kton-protocol/kton-cockpit/internal/flock"
)

// claimLocks serialises the callers in this process, by repository; the file lock, those of other
// processes.
var (
	claimLocksMu sync.Mutex
	claimLocks   = map[string]*sync.Mutex{}
)

// LockClaims takes the lock for appending a claim to this repository's nekton store. A writer holds
// it from reading where a claim goes (its scope's head, the claim it replaces) until the claim is
// stored, so two claims said at the same time, in one process or two, never take the same head
// (qm-dev-provenance, 20.11.: five concurrent cockpit_say calls forked a scope). The lock file lives
// in the temp directory, named by the repository's resolved path, so it is never part of the store.
func (r *Runner) LockClaims() (func(), error) {
	abs, err := filepath.Abs(r.cfg.RepoRoot)
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	claimLocksMu.Lock()
	mu, ok := claimLocks[abs]
	if !ok {
		mu = &sync.Mutex{}
		claimLocks[abs] = mu
	}
	claimLocksMu.Unlock()
	mu.Lock()
	sum := sha256.Sum256([]byte(abs))
	unlock, err := flock.Lock(filepath.Join(os.TempDir(), "kton-cockpit-claims-"+hex.EncodeToString(sum[:8])+".lock"))
	if err != nil {
		mu.Unlock()
		return nil, fmt.Errorf("locking %s for a claim: %w", r.cfg.RepoRoot, err)
	}
	return func() { unlock(); mu.Unlock() }, nil
}
