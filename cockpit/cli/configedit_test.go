package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Setting one value leaves every other key where it was, including one this build does not know.
func TestEditConfig_KeepsWhatItDoesNotSet(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cockpit.config.json")
	must(t, os.WriteFile(p, []byte(`{"zeta":1,"trust":{"tiers":{"peer":["a.pub"]},"x-note":"mine"},"alpha":{"b":true}}`), 0o644))
	must(t, editConfig(p, set([]string{"trust", "tiers", "self"}, []string{"s.pub"}), set([]string{"execution", "image"}, "oci://i@sha256:00")))
	b, _ := os.ReadFile(p)
	got := strings.Join(strings.Fields(string(b)), "")
	want := `{"zeta":1,"trust":{"tiers":{"peer":["a.pub"],"self":["s.pub"]},"x-note":"mine"},"alpha":{"b":true},"execution":{"image":"oci://i@sha256:00"}}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// An existing member is replaced in place, not appended a second time.
func TestEditConfig_ReplacesInPlace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cockpit.config.json")
	must(t, os.WriteFile(p, []byte(`{"identity":{"session_id":"session-1"},"z":0}`), 0o644))
	must(t, editConfig(p, set([]string{"identity"}, map[string]string{"session_id": "alice"})))
	b, _ := os.ReadFile(p)
	if got := strings.Join(strings.Fields(string(b)), ""); got != `{"identity":{"session_id":"alice"},"z":0}` {
		t.Errorf("got %s", got)
	}
}
