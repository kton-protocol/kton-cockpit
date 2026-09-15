package binaries

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A template's predicate is written out as a full IRI, never as a CURIE.
//
// An alias file resolves a prefix before the claim is built, so the signed record carries the full
// IRI either way — which is exactly the hazard: two repositories with different alias files produce
// different records from the same template text, and nothing in either record says which file
// decided. Writing the IRI out removes the resolution step instead of pinning it.
func TestTemplates_NamePredicatesByFullIRI(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine this source file's path")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file))) // internal/binaries/x_test.go -> root
	dir := filepath.Join(root, "uat", "participant-skeleton", "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatal(rerr)
		}
		var tmpl struct {
			Predicate string `json:"predicate"`
			Context   string `json:"context"`
		}
		if jerr := json.Unmarshal(b, &tmpl); jerr != nil {
			t.Fatalf("%s: %v", e.Name(), jerr)
		}
		seen++
		for field, v := range map[string]string{"predicate": tmpl.Predicate, "context": tmpl.Context} {
			if v == "" {
				continue
			}
			if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
				t.Errorf("%s: %s %q is a CURIE; write the full IRI so no alias file decides what it means",
					e.Name(), field, v)
			}
		}
	}
	// A directory that quietly emptied would otherwise pass having checked nothing.
	if seen == 0 {
		t.Fatalf("no templates found in %s", dir)
	}
}
