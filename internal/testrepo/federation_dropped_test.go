package testrepo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/source"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
)

// A source whose store holds a record its own load would refuse (here a claim line without its
// signature) does not give a clean-looking union with that record silently missing: the union reads
// every record on disk and judges it as Add does, so it fails naming the record, as a plankton record
// that does not load fails it (kton-review F1 on #198).
func TestFederation_ARecordASourceCannotLoadFailsTheUnion(t *testing.T) {
	theirs := New(t)
	tc := theirs.Config(t)
	if _, err := binaries.New(tc).Annotate(context.Background(), "urn:example:x", "working-on",
		map[string]string{"step": "a", "by-session": SessionID}, tc.NektonKey, nil); err != nil {
		t.Fatal(err)
	}
	var store string
	filepath.WalkDir(tc.NektonDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".nekton.jsonl") {
			if b, _ := os.ReadFile(p); len(b) > 0 {
				store = p
			}
		}
		return nil
	})
	if store == "" {
		t.Fatal("no nekton store file with a record")
	}
	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)[0]
	var line map[string]any
	if err := json.Unmarshal([]byte(first), &line); err != nil {
		t.Fatal(err)
	}
	stripped := false
	var strip func(v any)
	strip = func(v any) {
		if m, ok := v.(map[string]any); ok {
			if _, has := m["signatures"]; has {
				m["signatures"] = []any{}
				stripped = true
			}
			for _, x := range m {
				strip(x)
			}
		}
	}
	strip(line)
	if !stripped {
		t.Fatalf("no signatures in the store line: %s", first)
	}
	out, _ := json.Marshal(line)
	f, err := os.OpenFile(store, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(append(out, '\n'))
	f.Close()

	ours := New(t)
	oc := ours.Config(t)
	var spec source.Spec
	sb, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": tc.RepoRoot})
	if err := json.Unmarshal(sb, &spec); err != nil {
		t.Fatal(err)
	}
	oc.Raw.Federation.Sources = []source.Spec{spec}
	_, err = binaries.New(oc).About(context.Background(), "urn:example:x")
	if err == nil || !strings.Contains(err.Error(), "source theirs") {
		t.Errorf("a source with a record that does not load: want the union refused naming the source, got %v", err)
	}
}

// A source whose store holds a line that does not parse (half a line, as a torn write leaves) fails the
// union naming the source, rather than giving a union that looks whole without that record (kton-review
// F2 on #200; nekton Registry.Unreadable, kton v0.4.2).
func TestFederation_ALineASourceCannotParseFailsTheUnion(t *testing.T) {
	theirs := New(t)
	tc := theirs.Config(t)
	if _, err := binaries.New(tc).Annotate(context.Background(), "urn:example:x", "working-on",
		map[string]string{"step": "a", "by-session": SessionID}, tc.NektonKey, nil); err != nil {
		t.Fatal(err)
	}
	var store string
	filepath.WalkDir(tc.NektonDir, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".nekton.jsonl") {
			if b, _ := os.ReadFile(p); len(b) > 0 {
				store = p
			}
		}
		return nil
	})
	if store == "" {
		t.Fatal("no nekton store file with a record")
	}
	b, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)[0]
	f, err := os.OpenFile(store, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(first[:len(first)/2] + "\n") // half of a line
	f.Close()

	ours := New(t)
	oc := ours.Config(t)
	var spec source.Spec
	sb, _ := json.Marshal(map[string]string{"name": "theirs", "kind": "dir", "path": tc.RepoRoot})
	if err := json.Unmarshal(sb, &spec); err != nil {
		t.Fatal(err)
	}
	oc.Raw.Federation.Sources = []source.Spec{spec}
	_, err = binaries.New(oc).About(context.Background(), "urn:example:x")
	if err == nil || !strings.Contains(err.Error(), "source theirs") || !strings.Contains(err.Error(), "do not parse") {
		t.Errorf("a source with a line that does not parse: want the union refused naming the source, got %v", err)
	}
}
