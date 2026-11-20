package cockpit

import (
	"strings"
	"testing"
)

// A binding goes first into the outermost group, so a FILTER in that group sees it, and into the group
// of every sub-select (SPEC §9.9). Braces and keywords are found past comments, strings and IRIs that
// hold braces of their own.
func TestBind_GoesFirstIntoTheWhereClause(t *testing.T) {
	clause, err := valuesClause(map[string]string{"asOf": "2026-11-18"}, nil)
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
	} {
		got, err := bindIntoWhere(c.q, clause)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.HasPrefix(got, c.before+clause) || got != c.before+clause+strings.TrimPrefix(c.q, c.before) {
			t.Errorf("%s: the VALUES clause is not right after the WHERE clause's brace:\n%s", c.name, got)
		}
	}
	// A sub-select is evaluated on its own: the clause goes first into its group as well
	// (userinterface, 20.11.), and only for a SELECT keyword, not for one in a comment, a string, a
	// variable or a prefixed name.
	for _, c := range []struct{ name, q, want string }{
		{"a sub-select", "# dataset: claims\nSELECT * WHERE { { SELECT ?s WHERE { ?s ?p ?o } } }",
			"# dataset: claims\nSELECT * WHERE {" + clause + " { SELECT ?s WHERE {" + clause + " ?s ?p ?o } } }"},
		{"a sub-select without WHERE, lower case", "SELECT * { { select ?s { ?s ?p ?o } } }",
			"SELECT * {" + clause + " { select ?s {" + clause + " ?s ?p ?o } } }"},
		{"SELECT that is no keyword", "SELECT ?select WHERE { ?select ex:select \"select {\" # select {\n }",
			"SELECT ?select WHERE {" + clause + " ?select ex:select \"select {\" # select {\n }"},
		// a group is told from an expression's brace by the parentheses (kton-review E1 on #190)
		{"EXISTS in the outer SELECT's expressions", "SELECT ?doc (EXISTS { ?doc ?p ?o } AS ?f) WHERE { ?doc ?q ?r }",
			"SELECT ?doc (EXISTS { ?doc ?p ?o } AS ?f) WHERE {" + clause + " ?doc ?q ?r }"},
		{"EXISTS in a sub-select's expressions", "SELECT * WHERE { { SELECT ?f (EXISTS { ?d ?p ?o } AS ?e) WHERE { ?d ?q ?f } } }",
			"SELECT * WHERE {" + clause + " { SELECT ?f (EXISTS { ?d ?p ?o } AS ?e) WHERE {" + clause + " ?d ?q ?f } } }"},
		{"a prefixed name select before a group", "SELECT * WHERE { ?s ex:select ?o FILTER NOT EXISTS { ?s ?p ?o } }",
			"SELECT * WHERE {" + clause + " ?s ex:select ?o FILTER NOT EXISTS { ?s ?p ?o } }"},
	} {
		got, err := bindIntoWhere(c.q, clause)
		if err != nil || got != c.want {
			t.Errorf("%s:\ngot  %q\nwant %q (%v)", c.name, got, c.want, err)
		}
	}
	if _, err := bindIntoWhere("SELECT * # {\n", clause); err == nil {
		t.Error("a query without a WHERE clause must be refused, not bound at its end")
	}
}

// bindTerms binds a variable to a typed term: an IRI, or a literal with a datatype, written as one
// RDF term. Anything that is not one term is refused, never written into the query (SPEC §9.9;
// userinterface's PR 2: refine and params bind IRIs and integers).
func TestBind_TypedTermsAreOneTermEach(t *testing.T) {
	for want, term := range map[string]BoundTerm{
		`<https://kton.dev/o/abc>`:                                            {Type: "iri", Value: "https://kton.dev/o/abc"},
		`<urn:qms:doc:SOP-12>`:                                                {Type: "iri", Value: "urn:qms:doc:SOP-12"},
		`<https://x.org/a%20b>`:                                               {Type: "iri", Value: "https://x.org/a%20b"},
		`<https://x.org/a#b>`:                                                 {Type: "iri", Value: "https://x.org/a#b"},
		`"12"^^<http://www.w3.org/2001/XMLSchema#integer>`:                    {Type: "literal", Value: "12", Datatype: "xsd:integer"},
		`"2026-11-19"^^<http://www.w3.org/2001/XMLSchema#date>`:               {Type: "literal", Value: "2026-11-19", Datatype: "xsd:date"},
		`"2026-11-19T10:00:00Z"^^<http://www.w3.org/2001/XMLSchema#dateTime>`: {Type: "literal", Value: "2026-11-19T10:00:00Z", Datatype: "http://www.w3.org/2001/XMLSchema#dateTime"},
		`"x \" y"^^<https://example.org/t>`:                                   {Type: "literal", Value: `x " y`, Datatype: "https://example.org/t"},
		`"plain"`:                                                             {Type: "literal", Value: "plain"},
	} {
		clause, err := valuesClause(nil, map[string]BoundTerm{"V": term})
		if err != nil || !strings.Contains(clause, "{ ("+want+") }") {
			t.Errorf("%+v: want %s, got %q %v", term, want, clause, err)
		}
	}
	for name, term := range map[string]BoundTerm{
		"an IRI that closes the term":    {Type: "iri", Value: "https://x.org/> } ; DROP"},
		"an IRI with a space":            {Type: "iri", Value: "https://x.org/a b"},
		"an IRI with a brace":            {Type: "iri", Value: "https://x.org/}"},
		"a relative IRI":                 {Type: "iri", Value: "x.org/a"},
		"an integer with query text":     {Type: "literal", Value: "12 } ;", Datatype: "xsd:integer"},
		"a date that is not one":         {Type: "literal", Value: "19.11.2026", Datatype: "xsd:date"},
		"an xsd type bind does not know": {Type: "literal", Value: "1", Datatype: "xsd:foo"},
		"a datatype that closes":         {Type: "literal", Value: "1", Datatype: "https://x.org/> }"},
		"no type":                        {Value: "x"},
		"a % without two hex digits":     {Type: "iri", Value: "https://x.org/a%zz"},
		"a % at the end":                 {Type: "iri", Value: "https://x.org/a%4"},
		"DEL":                            {Type: "iri", Value: "https://x.org/a\x7fb"},
		"a line separator":               {Type: "iri", Value: "https://x.org/a\u2028b"},
		"a no-break space":               {Type: "iri", Value: "https://x.org/a\u00a0b"},
		"a second fragment":              {Type: "iri", Value: "https://x.org/a#b#c"},
	} {
		if clause, err := valuesClause(nil, map[string]BoundTerm{"V": term}); err == nil {
			t.Errorf("%s: want refused, got %q", name, clause)
		}
	}
	if _, err := valuesClause(map[string]string{"V": "a"}, map[string]BoundTerm{"V": {Type: "iri", Value: "https://x.org/"}}); err == nil {
		t.Error("a variable bound in bind and bindTerms must be refused")
	}
}

// A kton id bound as an IRI is the node it names in every dataset, https://kton.dev/o/<hex>; a
// sha256: value that is no kton id is refused, never bound as an IRI that matches nothing
// (kton-review B3 on ADR-018).
func TestBind_AKtonIDIsTheNodeItNames(t *testing.T) {
	hex := strings.Repeat("Ab", 32)
	got, err := BoundTerm{Type: "iri", Value: "sha256:" + hex}.sparqlTerm()
	if err != nil || got != "<https://kton.dev/o/"+strings.ToLower(hex)+">" {
		t.Errorf("a kton id: %q %v", got, err)
	}
	for _, bad := range []string{"sha256:abc", "SHA256:" + hex + "0"} {
		if got, err := (BoundTerm{Type: "iri", Value: bad}).sparqlTerm(); err == nil {
			t.Errorf("%q: want a refusal, got %q", bad, got)
		}
	}
	if got, err := (BoundTerm{Type: "iri", Value: "https://example.org/x"}).sparqlTerm(); err != nil || got != "<https://example.org/x>" {
		t.Errorf("any other IRI stays as written: %q %v", got, err)
	}
}
