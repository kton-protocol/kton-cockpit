package watch

import (
	"os"
	"path/filepath"
	"testing"
)

// mh4, 12.11.: an Rscript's reads of an R package library outside the project are its environment
func TestClass_PackageLibraryIsEnvironment(t *testing.T) {
	home := t.TempDir()
	lib := filepath.Join(home, "R", "merge-demo-lib")
	os.MkdirAll(filepath.Join(lib, "ggplot2", "Meta"), 0o755)
	os.WriteFile(filepath.Join(lib, "ggplot2", "Meta", "package.rds"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(home, "data"), 0o755)
	c := &Classifier{Project: filepath.Join(home, "proj"), System: DefaultSystem, Temp: DefaultTemp}
	for p, want := range map[string]Class{
		filepath.Join(lib, "ggplot2", "Meta", "package.rds"): ClassEnv,
		filepath.Join(lib, "ggplot2", "R", "ggplot2.rdb"):    ClassEnv,
		filepath.Join(home, ".venv", "lib", "python3.12", "site-packages", "pandas", "x.py"): ClassEnv,
		filepath.Join(home, "data", "delivery.csv"):         ClassOutside, // a real input outside the project stays one
		filepath.Join(home, "proj", "script", "gof.R"):      ClassProject,
	} {
		if got := c.Class(p); got != want {
			t.Errorf("%s: %s, want %s", p, got, want)
		}
	}
}
