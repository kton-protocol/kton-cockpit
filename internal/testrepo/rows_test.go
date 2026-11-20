package testrepo_test

import (
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A template that says "claims": "rows" is one of the row templates; one without it is not (ADR-018,
// section 5; ktonpkg v0.9.12).
func TestRowTemplates_ReadFromTheTemplatesClaimsField(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, ".kton/templates/covered.json", `{"name": "covered", "target": "either", "claims": "rows",
  "predicate": "https://example.org/q/covered", "fields": {"element": {"type": "string"}}}`)
	r.Write(t, ".kton/templates/decided.json", `{"name": "decided", "target": "either",
  "predicate": "https://example.org/q/decided", "fields": {"outcome": {"type": "string"}}}`)
	rows, err := binaries.New(r.Config(t)).RowTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if !rows["covered"] || rows["decided"] {
		t.Errorf("want covered alone: %v", rows)
	}
}
