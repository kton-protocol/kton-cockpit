package container

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// The masked directories exist before the engine runs, so it never makes them itself — as root,
// inside the repository — which left the registry unwritable after a first run in a container.
func TestEnsureMountPoints_MakesTheMaskedDirectories(t *testing.T) {
	root := t.TempDir()
	cfg := config.Layout(root, config.Raw{})
	if err := ensureMountPoints(cfg); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(root, ".kton")); err != nil || !fi.IsDir() {
		t.Errorf(".kton not made: %v", err)
	}
	// The negative control: .git is masked but not the cockpit's to make.
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Error(".git was made")
	}
}
