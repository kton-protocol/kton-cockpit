package index_test

import (
	"errors"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index"
)

// A query that calls another endpoint is refused wherever the grammar has the keyword, and a query
// that only mentions the word is not.
func TestCheckQuery_RefusesServiceAndOnlyService(t *testing.T) {
	refused := []string{
		`SELECT * { SERVICE <http://127.0.0.1:7878/query> { GRAPH ?g { ?s ?p ?o } } }`,
		`select * { service silent <http://x/> { ?s ?p ?o } }`,
		`SELECT * {SERVICE<http://x/>{?s ?p ?o}}`,
		`SELECT * { ?s ?p ?o . SeRvIcE ?endpoint { ?a ?b ?c } }`,
		`SELECT * { SERVICE <http://x/> { ?s ?p ?o } }`,
		`SELECT * { \U00000053ERVICE <http://x/> { ?s ?p ?o } }`,
		"SELECT * { # a comment\nSERVICE <http://x/> { ?s ?p ?o } }",
		`SELECT * { FILTER(?a < 3) SERVICE <http://x/> { ?s ?p ?o } }`,
	}
	for _, q := range refused {
		if err := index.CheckQuery(q); !errors.Is(err, index.ErrService) {
			t.Errorf("not refused (%v): %s", err, q)
		}
	}
	allowed := []string{
		`SELECT ?service { ?s <http://x/service> ?service }`,
		`SELECT $SERVICE { ?s ?p $SERVICE }`,
		`PREFIX SERVICE: <http://x/> SELECT * { ?s SERVICE:p ?o }`,
		`PREFIX ex: <http://x/> SELECT * { ?s ex:SERVICE ?o }`,
		`SELECT * { ?s ?p "SERVICE <http://x/> { }" }`,
		`SELECT * { ?s ?p '''a
SERVICE'''' }`,
		"SELECT * { ?s ?p ?o } # SERVICE <http://x/>",
		`SELECT * { <http://x/SERVICE> ?p ?o }`,
		`SELECT * { ?s ?p ?o . ?o ?q "SERVICE" }`,
		`SELECT * { ?s a ?servicePoint }`,
	}
	for _, q := range allowed {
		if err := index.CheckQuery(q); err != nil {
			t.Errorf("refused (%v): %s", err, q)
		}
	}
	for _, q := range []string{`SELECT * { ?s ?p "open }`, `SELECT * { ?s ?p """open }`, `SELECT * { \u00 }`} {
		if err := index.CheckQuery(q); err == nil {
			t.Errorf("a query that cannot be read to its end was accepted: %s", q)
		}
	}
}

// A query someone wrote is answered when it is a SELECT or an ASK over the dataset the cockpit
// chooses: not an update, not a graph-building form, not one that names its own dataset.
func TestCheckFreeQuery_TakesSelectAndAskOverTheCockpitsDataset(t *testing.T) {
	for q, want := range map[string]string{
		`SELECT * { ?s ?p ?o }`: "SELECT",
		"PREFIX prov: <http://www.w3.org/ns/prov#>\nBASE <http://x/>\nselect ?a { ?a a prov:Activity }": "SELECT",
		`PREFIX ask: <http://x/> ASK { ?s ask:p ?o }`:                                                   "ASK",
		`# CONSTRUCT { } FROM <http://x/>` + "\nASK { ?s ?p \"FROM\" }":                                 "ASK",
		`SELECT ?from { ?s <http://x/from> ?from }`:                                                     "SELECT",
		`SELECT * { { SELECT ?s { ?s ?p ?o } LIMIT 3 } }`:                                               "SELECT",
	} {
		if got, err := index.CheckFreeQuery(q); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %s", q, got, err, want)
		}
	}
	for _, q := range []string{
		`CONSTRUCT { ?s ?p ?o } WHERE { ?s ?p ?o }`,
		`DESCRIBE <http://x/>`,
		`INSERT DATA { <http://x/a> <http://x/b> <http://x/c> }`,
		`PREFIX ex: <http://x/> DELETE WHERE { ?s ex:p ?o }`,
		`SELECT * FROM <https://kton.dev/o/00> { ?s ?p ?o }`,
		`SELECT * FROM NAMED <https://kton.dev/np/00.#assertion> { GRAPH ?g { ?s ?p ?o } }`,
		`SELECT * { SERVICE <http://x/> { ?s ?p ?o } }`,
		`PREFIX ex: <http://x/>`,
		``,
	} {
		if form, err := index.CheckFreeQuery(q); err == nil {
			t.Errorf("answered as %s: %q", form, q)
		}
	}
	if _, err := index.CheckFreeQuery(`SELECT * FROM <http://x/> { }`); !errors.Is(err, index.ErrFrom) {
		t.Errorf("FROM refused for the wrong reason: %v", err)
	}
}
