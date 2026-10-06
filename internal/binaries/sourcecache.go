package binaries

import (
	"path/filepath"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// SourceCache is where a federation source keeps what it fetched — the directory ReadDirs hands its
// kind. Outside the repository, like the union.
func SourceCache(cfg *config.Config, name string) (string, error) {
	base, err := cacheDir(cfg)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "sources", name), nil
}
