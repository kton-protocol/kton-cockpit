package oxigraph

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index"
)

// free.go answers a query someone wrote (ask's sparql) rather than one an allowed package brings.
// It reads the dataset Query reads, and returns the protocol's JSON results, which carry the
// variables in their order and the answer of an ASK, where CSV carries neither.

// Results is a SPARQL answer: the variables and rows of a SELECT, or the boolean of an ASK. A value
// is its lexical form: an IRI, a literal's text, a blank node's label.
type Results struct {
	Vars    []string            `json:"vars"`
	Rows    []map[string]string `json:"rows"`
	Boolean *bool               `json:"boolean,omitempty"`
	// Terms are Rows as the protocol gave them: each value with its type, datatype and language.
	Terms []map[string]Term `json:"-"`
}

// Term is one bound value of SPARQL 1.1 Query Results JSON.
type Term struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Datatype string `json:"datatype,omitempty"`
	Lang     string `json:"xml:lang,omitempty"`
}

// FreeAnswer is the answer to a free query and how the registries behind it were read.
type FreeAnswer struct {
	Results
	Registries int
	Kept       bool
}

// ErrTimeout is a free query cut off at its timeout.
var ErrTimeout = errors.New("the query ran longer than its timeout")

// Datasets a free query reads (ADR-013): the query projection, where a claim is a node, or the
// kernel's projection, where it is a nanopublication.
const (
	DatasetClaims  = "claims"
	DatasetNanopub = "nanopub"
)

// Free answers a SELECT or an ASK over the union of cfg, under keys, in dataset. timeout bounds the
// evaluation, not the reading of the registries into the store, which only adds and is never left
// half done.
func Free(ctx context.Context, d *Daemon, cfg *config.Config, keys []ed25519.PublicKey, query, dataset string, timeout time.Duration) (*FreeAnswer, error) {
	if _, err := index.CheckFreeQuery(query); err != nil {
		return nil, err
	}
	ds, err := d.prepare(ctx, cfg, keys)
	if err != nil {
		return nil, err
	}
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	form := datasetForm(ds.def, ds.named, query)
	if dataset != DatasetNanopub {
		form = datasetForm(ds.claims, nil, query)
	}
	body, err := d.post(qctx, "/query", "application/sparql-results+json", form)
	if qctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%w (%s)", ErrTimeout, timeout)
	}
	if err != nil {
		return nil, err
	}
	res, err := ParseResults(body)
	if err != nil {
		return nil, err
	}
	return &FreeAnswer{Results: *res, Registries: ds.registries, Kept: ds.kept}, nil
}

// ParseResults reads SPARQL 1.1 query results in JSON.
func ParseResults(body []byte) (*Results, error) {
	var raw struct {
		Head struct {
			Vars []string `json:"vars"`
		} `json:"head"`
		Results *struct {
			Bindings []map[string]Term `json:"bindings"`
		} `json:"results"`
		Boolean *bool `json:"boolean"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("reading the engine's answer: %w", err)
	}
	res := &Results{Vars: raw.Head.Vars, Rows: []map[string]string{}, Boolean: raw.Boolean}
	if res.Vars == nil {
		res.Vars = []string{}
	}
	if raw.Results != nil {
		for _, b := range raw.Results.Bindings {
			row := map[string]string{}
			for k, v := range b {
				row[k] = v.Value
			}
			res.Rows = append(res.Rows, row)
			res.Terms = append(res.Terms, b)
		}
	}
	return res, nil
}
