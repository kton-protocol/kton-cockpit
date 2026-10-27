package cockpit

import (
	"context"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
)

// A free query (SPEC §9.9) is SPARQL someone wrote, read over the dataset a vocabulary query reads:
// every plankton record of the union, attributed only where a key of this repository's trust tiers
// verifies it, and the claims such a key signed. It narrows itself, so ref and filter are refused
// beside it, and what it may not do is checked before it runs: only SELECT and ASK, no SERVICE, no
// FROM.

// SparqlAnswer is a free query's answer.
type SparqlAnswer struct {
	// Form is SELECT or ASK.
	Form string `json:"form"`
	// Vars are a SELECT's variables in the query's order; Rows bind them by name, each value in its
	// lexical form (an IRI, a literal's text). A content node is https://kton.dev/o/<hex>, the
	// record or file sha256:<hex>.
	Vars []string            `json:"vars"`
	Rows []map[string]string `json:"rows"`
	// Boolean is an ASK's answer.
	Boolean *bool `json:"boolean,omitempty"`
	// Results is the same answer in the standard SPARQL 1.1 Query Results JSON, where every value
	// keeps its type (uri, literal, bnode), datatype and language; only with format
	// "sparql-results+json".
	Results *SparqlResults `json:"results,omitempty"`
	// Truncated says Rows stop at the limit and the query had more.
	Truncated bool `json:"truncated,omitempty"`
	Limit     int  `json:"limit"`
	// Via says where it was evaluated: the index daemon and how many registries it read, or a
	// container for this query alone.
	Via string `json:"via"`

	terms []map[string]oxigraph.Term
}

func (a *SparqlAnswer) standard() *SparqlResults {
	r := &SparqlResults{Boolean: a.Boolean}
	if a.Boolean != nil {
		return r
	}
	r.Head.Vars = a.Vars
	r.Results = &struct {
		Bindings []map[string]SparqlTerm `json:"bindings"`
	}{Bindings: []map[string]SparqlTerm{}}
	for _, row := range a.terms {
		b := map[string]SparqlTerm{}
		for k, t := range row {
			b[k] = SparqlTerm{Type: t.Type, Value: t.Value, Datatype: t.Datatype, Lang: t.Lang}
		}
		r.Results.Bindings = append(r.Results.Bindings, b)
	}
	return r
}

// SparqlResults is a document in SPARQL 1.1 Query Results JSON (W3C).
type SparqlResults struct {
	Head struct {
		Vars []string `json:"vars,omitempty"`
	} `json:"head"`
	Results *struct {
		Bindings []map[string]SparqlTerm `json:"bindings"`
	} `json:"results,omitempty"`
	Boolean *bool `json:"boolean,omitempty"`
}

// SparqlTerm is one bound value: type uri, literal or bnode, its value, and a literal's datatype or
// language.
type SparqlTerm struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Datatype string `json:"datatype,omitempty"`
	Lang     string `json:"xml:lang,omitempty"`
}

// SparqlFormatStandard asks for Results beside Rows.
const SparqlFormatStandard = "sparql-results+json"

const (
	sparqlLimit      = 1000
	sparqlMaxLimit   = 10000
	sparqlTimeout    = 30 * time.Second
	sparqlMaxTimeout = 120 * time.Second
)

func askSparql(ctx context.Context, cfg *config.Config, in AskRequest) (*AskResult, error) {
	switch {
	case in.Sparql == "":
		return nil, refuse("argument", "SPEC §9.9", "query sparql takes sparql: a SELECT or an ASK")
	case in.Ref != "" || len(in.Refs) > 0:
		return nil, refuse("argument", "SPEC §9.9", "query sparql takes no ref: the query names what it asks about")
	case in.Filter.active():
		return nil, refuse("filter.invalid", "SPEC §9.9", "query sparql takes no filter: the query narrows itself (prov:wasAttributedTo is the signer)")
	case in.Limit < 0 || in.Limit > sparqlMaxLimit:
		return nil, refuse("argument", "SPEC §9.9", "limit must be from 1 to %d, not %d", sparqlMaxLimit, in.Limit)
	case in.Format != "" && in.Format != SparqlFormatStandard:
		return nil, refuse("argument", "SPEC §9.9", "format is %q or empty, not %q", SparqlFormatStandard, in.Format)
	case in.TimeoutSeconds < 0 || time.Duration(in.TimeoutSeconds)*time.Second > sparqlMaxTimeout:
		return nil, refuse("argument", "SPEC §9.9", "timeoutSeconds must be from 1 to %d, not %d", int(sparqlMaxTimeout/time.Second), in.TimeoutSeconds)
	}
	limit, timeout := in.Limit, time.Duration(in.TimeoutSeconds)*time.Second
	if limit == 0 {
		limit = sparqlLimit
	}
	if timeout == 0 {
		timeout = sparqlTimeout
	}
	ans, err := runSparql(ctx, cfg, in.Sparql, timeout)
	if err != nil {
		return nil, err
	}
	ans.Limit = limit
	if len(ans.Rows) > limit {
		ans.Rows, ans.terms, ans.Truncated = ans.Rows[:limit], ans.terms[:limit], true
	}
	if in.Format == SparqlFormatStandard {
		ans.Results = ans.standard()
	}
	return &AskResult{Query: in.Query, Sparql: ans,
		FilterApplied: "none — the query narrows itself; a record is attributed only where a key in your trust tiers verifies it, and only claims such a key signed are in it"}, nil
}
