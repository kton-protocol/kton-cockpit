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

// The machine's index is recognised by its directory and its daemon's address, not by how the
// configuration names it; another directory or address is not it (kton-review S1 on #215).
func TestSharedIndex_ByDirectoryAndAddress(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	machine := filepath.Join(cache, "kton-cockpit", "index")
	if err := os.MkdirAll(machine, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(machine, "daemon.json"), []byte(`{"url": "http://127.0.0.1:1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := func(url string) *config.Config {
		c := &config.Config{}
		c.Raw.Index.URL = url
		return c
	}
	for _, c := range []struct {
		env, url string
		shared   bool
	}{
		{"", "", true},
		{machine + "/", "", true},
		{t.TempDir(), "", false},
		{"", "http://127.0.0.1:1", true},
		{"", "http://127.0.0.1:2", false},
	} {
		t.Setenv("COCKPIT_INDEX_DIR", c.env)
		if got := SharedIndex(cfg(c.url)); got != c.shared {
			t.Errorf("COCKPIT_INDEX_DIR=%q index.url=%q: shared %v, want %v", c.env, c.url, got, c.shared)
		}
	}
}
