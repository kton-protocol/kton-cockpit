package cockpit

import (
	"strings"
	"testing"
)

// A binding goes first into the outermost group, so a FILTER in that group sees it (SPEC §9.9). The
// brace is found past comments, strings and IRIs that hold braces of their own.
func TestBind_GoesFirstIntoTheWhereClause(t *testing.T) {
	clause, err := valuesClause(map[string]string{"asOf": "2026-11-18"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, q, before string }{
		{"plain", "SELECT ?x WHERE { ?x ?p ?o FILTER(?o < xsd:date(?asOf)) }", "SELECT ?x WHERE {"},
		{"no WHERE keyword", "ASK { ?x ?p ?o }", "ASK {"},
		{"a brace in a comment", "# {not this}\nSELECT * WHERE { ?s ?p ?o }", "# {not this}\nSELECT * WHERE {"},
		{"a hash in an IRI, braces in strings", "PREFIX a: <https://x.org/ns#> SELECT (\"{\" AS ?b) ('}' AS ?c) WHERE { ?s ?p ?o }",
			"PREFIX a: <https://x.org/ns#> SELECT (\"{\" AS ?b) ('}' AS ?c) WHERE {"},
		{"a long string with a quote and a brace", "SELECT (\"\"\"a \" { \\\"\"\" b\"\"\" AS ?b) WHERE { ?s ?p ?o }",
			"SELECT (\"\"\"a \" { \\\"\"\" b\"\"\" AS ?b) WHERE {"},
		{"less-than before the WHERE", "SELECT (IF(?a <3, 1, 2) AS ?b) WHERE { ?s ?p ?a }", "SELECT (IF(?a <3, 1, 2) AS ?b) WHERE {"},
		{"a dataset line and a subquery", "# dataset: claims\nSELECT * WHERE { { SELECT ?s WHERE { ?s ?p ?o } } }", "# dataset: claims\nSELECT * WHERE {"},
	} {
		got, err := bindIntoWhere(c.q, clause)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.HasPrefix(got, c.before+clause) || got != c.before+clause+strings.TrimPrefix(c.q, c.before) {
			t.Errorf("%s: the VALUES clause is not right after the WHERE clause's brace:\n%s", c.name, got)
		}
	}
	if _, err := bindIntoWhere("SELECT * # {\n", clause); err == nil {
		t.Error("a query without a WHERE clause must be refused, not bound at its end")
	}
}
