package container

import (
	"os"
	"path/filepath"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// ensureMountPoints makes the cockpit's own masked directories before the engine sees them. A
// masked path that does not exist yet is created by the engine, inside the repository bind mount,
// as root — and the first record written after a container run then fails with "permission denied"
// on registry/plankton/objects. Made here, they belong to the operator like everything else.
func ensureMountPoints(cfg *config.Config) error {
	for _, rel := range []string{cfg.Raw.Paths.KeysDir, cfg.Raw.Paths.BinDir, cfg.Raw.Paths.PlanktonDir, cfg.Raw.Paths.NektonDir} {
		if rel == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(cfg.RepoRoot, filepath.FromSlash(rel)), 0o755); err != nil {
			return err
		}
	}
	return nil
}
