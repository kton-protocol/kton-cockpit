package oxigraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// The memory a start asks for: the index directory's memory file as the floor, a repository's
// index.memory above it, the default below both; a file that is no limit is named and not used
// (cockpit-runtime, 20.11.).
func TestWanted_TheMemoryFileIsTheFloor(t *testing.T) {
	dir := t.TempDir()
	cfg := func(mem string) *config.Config {
		c := &config.Config{}
		c.Raw.Index.Memory = mem
		return c
	}
	if l, from, p := Wanted(cfg(""), dir); l != "2g" || from != "default" || p != "" {
		t.Errorf("no file: %s %s %q", l, from, p)
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, MemoryFile), []byte("4g\n"), 0o644))
	if l, from, p := Wanted(cfg(""), dir); l != "4g" || from != filepath.Join(dir, MemoryFile) || p != "" {
		t.Errorf("the file above the default: %s %s %q", l, from, p)
	}
	if l, from, _ := Wanted(cfg("8g"), dir); l != "8g" || from != "repository" {
		t.Errorf("a repository above the file: %s %s", l, from)
	}
	if l, from, _ := Wanted(cfg("1g"), dir); l != "4g" || from != filepath.Join(dir, MemoryFile) {
		t.Errorf("a repository below the file: %s %s", l, from)
	}
	must(os.WriteFile(filepath.Join(dir, MemoryFile), []byte("lots"), 0o644))
	if l, from, p := Wanted(cfg(""), dir); l != "2g" || from != "default" || !strings.Contains(p, `"lots"`) {
		t.Errorf("a file that is no limit is named and not used: %s %s %q", l, from, p)
	}
}
