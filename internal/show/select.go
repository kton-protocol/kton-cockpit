package show

// select.go is /data/select: a portion of the store for a viewer, chosen by a relation instead of
// handed over whole. A union.json of a large kton is too big to fetch, and kton-web stops reading
// at a record cap, so a viewer asks for the part it is drawing and expands from there.
//
// The selection decides only what is SHOWN. It may come from an index nobody signed, so every
// record goes out with its signed envelope and the keys of this repository's trust tiers, and the
// viewer verifies each one as it does a union.json. A selection that left a record out would show
// less; it could never make a record pass that does not verify.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/index"
	"kton.dev/plankton/core"
)

// Selection is one page of a selection: union.json's records, the ring to verify them with, and a
// cursor for the next page when the selection was cut.
type Selection struct {
	Query   string            `json:"query"`
	Records []Selected        `json:"records"`
	Keys    map[string]string `json:"keys"`
	Names   map[string]string `json:"names"`
	More    string            `json:"more,omitempty"`
}

// Selected is a record as union.json carries it, without the registry-local seq: a selection is
// drawn from a union, where a seq means nothing.
type Selected struct {
	FotonID  string          `json:"fotonId,omitempty"`
	ClaimID  string          `json:"claimId,omitempty"`
	Envelope json.RawMessage `json:"envelope"`
}

const (
	defaultLimit = 500
	maxLimit     = 5000
	maxHops      = 32
)

// selectRequest is a parsed /data/select query.
type selectRequest struct {
	q      string
	ids    []string // record: the ids; scope: the scope
	hash   string   // pedigree, uses
	hops   int
	offset int
	limit  int
}

func parseSelect(req *http.Request) (*selectRequest, error) {
	v := req.URL.Query()
	s := &selectRequest{q: v.Get("q"), ids: v["id"], hash: v.Get("hash"), limit: defaultLimit}
	num := func(name string, def, max int) (int, error) {
		raw := v.Get(name)
		if raw == "" {
			return def, nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > max {
			return 0, fmt.Errorf("%s must be a number from 0 to %d, not %q", name, max, raw)
		}
		return n, nil
	}
	var err error
	if s.limit, err = num("limit", defaultLimit, maxLimit); err != nil {
		return nil, err
	}
	if s.limit == 0 {
		s.limit = defaultLimit
	}
	if s.offset, err = num("cursor", 0, int(^uint(0)>>1)); err != nil {
		return nil, err
	}
	switch s.q {
	case "record":
		if len(s.ids) == 0 {
			return nil, fmt.Errorf("q=record takes one or more id=")
		}
	case "scope":
		if len(s.ids) != 1 {
			return nil, fmt.Errorf("q=scope takes one id= (the scope)")
		}
	case "pedigree", "uses":
		if s.hash == "" {
			return nil, fmt.Errorf("q=%s takes hash=", s.q)
		}
		def := 3
		if s.q == "uses" {
			def = 1
		}
		if s.hops, err = num("hops", def, maxHops); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("q must be one of record, pedigree, uses, scope, not %q", s.q)
	}
	return s, nil
}

// serveSelect answers one page of a selection.
func (s *Server) serveSelect(w http.ResponseWriter, req *http.Request) {
	sr, err := parseSelect(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sel, err := s.Select(req.Context(), index.Open(s.cfg), sr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sel)
}

// Select gathers the ids of a selection in a stable order, cuts the requested page, and fetches
// those records' envelopes.
func (s *Server) Select(ctx context.Context, r index.Reader, sr *selectRequest) (*Selection, error) {
	keys, names, err := s.ring(ctx)
	if err != nil {
		return nil, err
	}
	g := &gather{ctx: ctx, r: r, seen: map[string]bool{}, want: sr.offset + sr.limit + 1}
	switch sr.q {
	case "record":
		for _, id := range sr.ids {
			if err := g.recordWithClaims(id); err != nil {
				return nil, err
			}
		}
	case "pedigree", "uses":
		if err := g.walk(sr.q, sr.hash, sr.hops); err != nil {
			return nil, err
		}
	case "scope":
		if err := g.scope(sr.ids[0]); err != nil {
			return nil, err
		}
	}

	page := g.order
	if sr.offset >= len(page) {
		page = nil
	} else {
		page = page[sr.offset:]
	}
	sel := &Selection{Query: sr.q, Records: []Selected{}, Keys: keys, Names: names}
	if len(page) > sr.limit {
		page = page[:sr.limit]
		sel.More = strconv.Itoa(sr.offset + sr.limit)
	}
	for _, it := range page {
		env := it.envelope
		if env == nil {
			if env, err = r.EnvelopeFor(ctx, it.id); err != nil {
				return nil, fmt.Errorf("the envelope of %s: %w", it.id, err)
			}
		}
		rec := Selected{Envelope: env}
		if it.foton {
			rec.FotonID = it.id
		} else {
			rec.ClaimID = it.id
		}
		sel.Records = append(sel.Records, rec)
	}
	return sel, nil
}

// item is one record in a selection's order. A scope's claims carry their envelope already: one
// whose predecessor is not held is stored but cannot be fetched by id.
type item struct {
	id       string
	foton    bool
	envelope json.RawMessage
}

// gather collects a selection's records, each once, until it holds want of them: a page needs no
// more than its offset, its limit and one to know whether there is more.
type gather struct {
	ctx   context.Context
	r     index.Reader
	seen  map[string]bool
	order []item
	want  int
}

func (g *gather) full() bool { return len(g.order) >= g.want }

func (g *gather) add(it item) {
	if !g.seen[it.id] && !g.full() {
		g.seen[it.id] = true
		g.order = append(g.order, it)
	}
}

// recordWithClaims adds a record and, for a foton, the claims about it: expanding a node in a
// viewer means seeing what was said about it too.
func (g *gather) recordWithClaims(id string) error {
	if _, err := g.r.Foton(g.ctx, id); err != nil {
		// Not a foton: a claim, by id, or nothing this store holds.
		if _, err := g.r.EnvelopeFor(g.ctx, id); err != nil {
			return fmt.Errorf("no record %s here: %w", id, err)
		}
		g.add(item{id: id})
		return nil
	}
	g.add(item{id: id, foton: true})
	claims, err := g.r.About(g.ctx, id)
	if err != nil {
		return err
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].ID < claims[j].ID })
	for _, c := range claims {
		g.add(item{id: c.ID})
	}
	return nil
}

// walk goes breadth first from hash: up through the producers of each input (pedigree), or down
// through what used each output (uses). Level 0 is the producers (or users) of hash itself; hops
// is how many levels beyond it. Each level is sorted, so a page is the
// same page on every request while the store is unchanged.
func (g *gather) walk(relation, hash string, hops int) error {
	frontier := []string{hash}
	visited := map[string]bool{hash: true}
	for level := 0; level <= hops && len(frontier) > 0 && !g.full(); level++ {
		var fotons []string
		for _, h := range frontier {
			var res *binaries.LineageResult
			var err error
			if relation == "pedigree" {
				res, err = g.r.Producer(g.ctx, h)
			} else {
				res, err = g.r.Uses(g.ctx, h)
			}
			if err != nil {
				return err
			}
			for _, rec := range res.Records {
				fotons = append(fotons, rec.ID)
			}
		}
		sort.Strings(fotons)
		var next []string
		for _, id := range fotons {
			if g.seen[id] {
				continue
			}
			if err := g.recordWithClaims(id); err != nil {
				return err
			}
			f, err := g.r.Foton(g.ctx, id)
			if err != nil {
				return err
			}
			refs := f.Inputs
			if relation == "uses" {
				refs = f.Outputs
			}
			for _, h := range hashesOf(refs) {
				if !visited[h] {
					visited[h] = true
					next = append(next, h)
				}
			}
		}
		sort.Strings(next)
		frontier = next
	}
	return nil
}

func hashesOf(refs []core.FileRef) []string {
	var out []string
	for _, ref := range refs {
		if ref.Hash != "" {
			out = append(out, ref.Hash)
		}
	}
	return out
}

// scope adds a scope's seed and its chain, in the order the store received them.
func (g *gather) scope(id string) error {
	seed, err := g.r.Seed(g.ctx, id)
	if err != nil {
		return err
	}
	g.add(item{id: seed.ID, envelope: seed.Envelope})
	chain, err := g.r.ScopeChain(g.ctx, id)
	if err != nil {
		return err
	}
	for _, c := range chain {
		g.add(item{id: c.ID, envelope: c.Envelope})
	}
	return nil
}
