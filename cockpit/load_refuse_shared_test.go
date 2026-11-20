package cockpit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A load into the machine's shared index, all its files together, is bounded (16 MiB unless
// index.derived.maxBytesShared says otherwise), refused before the daemon is asked anything, however
// the configuration names that index: by default, by COCKPIT_INDEX_DIR spelled another way, or by the
// address its daemon runs at (qm-dev-provenance and cockpit-runtime, 20.11.; kton-review S1 on #215).
func TestLoad_TheSharedIndexTakesABoundedLoad(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache) // the machine's index directory, here a temporary one
	machine := filepath.Join(cache, "kton-cockpit", "index")
	if err := os.MkdirAll(machine, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "same-index")
	if err := os.Symlink(machine, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(machine, "daemon.json"), []byte(`{"url": "http://127.0.0.1:1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, env, url string }{
		{"by default", "", ""},
		{"COCKPIT_INDEX_DIR naming it through a link", link, ""},
		{"index.url at its daemon", "", "http://127.0.0.1:1/"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("COCKPIT_INDEX_DIR", c.env)
			r := testrepo.New(t)
			raw := testrepo.DefaultConfig()
			raw.Query.Image = "oci://ghcr.io/oxigraph/oxigraph@sha256:7532c1f9aa5f28c0dc6a4243198f7f6144017fab7429bf6c164a89fec9411767" // never started here
			raw.Index.Kind = "oxigraph"
			raw.Index.URL = c.url
			raw.Index.Derived.Enabled = true
			limit := int64(64)
			raw.Index.Derived.MaxBytesShared = &limit
			r.WriteConfig(t, raw)
			r.Use(t)
			r.Write(t, "rdf/a.ttl", "<https://example.org/a> <https://example.org/b> \"0123456789012345678901234567890123456789\" .\n")
			r.Write(t, "rdf/b.ttl", "<https://example.org/c> <https://example.org/d> \"e\" .\n")
			_, err := New(Start{}).Load(context.Background(), LoadRequest{Files: []string{"rdf/a.ttl", "rdf/b.ttl"}})
			var ref *Refusal
			if !errors.As(err, &ref) || ref.Code != "derived.shared-too-large" {
				t.Errorf("two files over the shared limit together: want derived.shared-too-large, got %v", err)
			}
		})
	}
}
