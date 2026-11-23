package cockpit

import "testing"

// A query run refuses what makes its answer depend on the moment or on evaluation order, decided on
// the query's words, not inside strings, IRIs or comments (ADR-020; kton-review A1, A4).
func TestCheckReproducible(t *testing.T) {
	for q, ok := range map[string]bool{
		`SELECT ?s WHERE { ?s ?p ?o }`:                                                       true,
		`SELECT ?s WHERE { ?s ?p "NOW()" }`:                                                  true,
		`SELECT ?s WHERE { ?s <https://ex.org/now(> ?o } # RAND()`:                           true,
		`SELECT ?t WHERE { BIND(NOW() AS ?t) }`:                                              false,
		`SELECT ?t WHERE { { SELECT (now() AS ?t) WHERE {} } }`:                              false,
		`SELECT (SAMPLE(?o) AS ?x) WHERE { ?s ?p ?o }`:                                       false,
		`SELECT (GROUP_CONCAT(?o) AS ?x) WHERE { ?s ?p ?o }`:                                 false,
		`SELECT ?s WHERE { ?s ?p ?o } LIMIT 10`:                                              false,
		`SELECT ?s ?o WHERE { ?s ?p ?o } ORDER BY ?s LIMIT 10`:                               false,
		`SELECT ?s ?o WHERE { ?s ?p ?o } ORDER BY ?s DESC(?o) LIMIT 10`:                      true,
		`SELECT * WHERE { ?s ?p ?o } ORDER BY ?s LIMIT 10`:                                   false,
		`SELECT ?s WHERE { { SELECT ?s WHERE { ?s ?p ?o } LIMIT 1 } }`:                       false,
		`SELECT ?s (COUNT(?o) AS ?n) WHERE { ?s ?p ?o } GROUP BY ?s ORDER BY ?s ?n OFFSET 2`: true,
		`SELECT ?s ?var_limit WHERE { ?s ?p ?var_limit }`:                                    true,
		`SELECT ?x WHERE { ?x ?p ?o } ORDER BY STRLEN(STR(?x)) LIMIT 1`:                      false,
		`SELECT ?x WHERE { ?x ?p ?o } ORDER BY (?x = ?x) LIMIT 1`:                            false,
		`SELECT ?x WHERE { ?x ?p ?o } ORDER BY STRLEN(STR(?x))`:                              true,
	} {
		if err := checkReproducible(q); (err == nil) != ok {
			t.Errorf("%s: want ok=%v, got %v", q, ok, err)
		}
	}
}
