//go:build rpython

package watch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Needs Rscript and python3: go test -tags rpython ./watch. Not in the default suite, which
// must not skip, and CI has no R.
// Review P6 of #18: system2 pastes its arguments into a shell line. The generated lines run
// with real R and python3; spaces, quotes, newlines, ; and $ reach Python as they were.
func TestPythonStep_RunsWithRealRAndPython(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "py"), 0o755)
	os.WriteFile(filepath.Join(dir, "py", "my step.py"), []byte("import sys\nopen('o2.txt', 'w').write(sys.argv[1])\n"), 0o644)
	script := fmt.Sprintf("setwd(%q)\n", dir) +
		pythonStep("x = 1\nopen('o1.txt', 'w').write(str(x))") +
		pythonStep(`%run -- "py/my step.py" "a b"`) +
		pythonStep(`open("o3.txt", "w").write("it's; $HOME")`)
	os.WriteFile(filepath.Join(dir, "recipe.R"), []byte(script), 0o644)
	if out, err := exec.Command("Rscript", filepath.Join(dir, "recipe.R")).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out, script)
	}
	for f, want := range map[string]string{"o1.txt": "1", "o2.txt": "a b", "o3.txt": "it's; $HOME"} {
		if b, _ := os.ReadFile(filepath.Join(dir, f)); string(b) != want {
			t.Errorf("%s = %q, want %q", f, b, want)
		}
	}
}
