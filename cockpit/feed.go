package cockpit

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/verify"
)

// Query "feed" (SPEC §9.10) is the union's records in the order they arrived, from a cursor: what
// an application watching the work asks instead of walking the registry's files (mignon's handoff
// 2.2). Each record comes with its verdict — the trusted key that verifies it, its tier and the
// name the trust configuration gives it — so no program checks a signature itself. A record no
// trusted key verifies is counted, never shown (SPEC §9.1).
//
// The cursor is each registry's position in the kernel's feed (its numbering's epoch and the last
// seq read). A registry whose numbering changed is read again from the start; one that did not move
// is not opened.

// FeedPage is the answer to query "feed".
type FeedPage struct {
	Records []FeedRecord `json:"records"`
	// Untrusted counts the records past the cursor that no key in the trust tiers verifies.
	Untrusted int `json:"untrusted"`
	// Cursor is where the next call starts; More says this page stopped at the limit.
	Cursor string `json:"cursor"`
	More   bool   `json:"more,omitempty"`
}

// FeedRecord is one record of the union and its verdict. A record two registries of the union hold
// comes once from each; its id is the same.
type FeedRecord struct {
	Kind     string          `json:"kind"` // foton or claim
	ID       string          `json:"id"`
	Registry string          `json:"registry"` // own, or the federation source's name
	Signer   FeedSigner      `json:"signer"`
	Envelope json.RawMessage `json:"envelope"`
}

// FeedSigner is the trusted key that verifies a record: its keyid, tier, and the name its public
// key file gives it in the trust configuration.
type FeedSigner struct {
	KeyID string `json:"keyid"`
	Tier  string `json:"tier"`
	Name  string `json:"name"`
}

const feedLimit = 1000

// feedPos is where the cursor stands in one registry: its numbering, the last seq read, and the
// stamp of its object files then (core.Position.Objects), since records not numbered yet change the
// stamp before they change the numbering.
type feedPos struct {
	Epoch   string `json:"e"`
	Seq     int    `json:"s"`
	Objects string `json:"o,omitempty"`
}

func askFeed(ctx context.Context, cfg *config.Config, in AskRequest) (*AskResult, error) {
	switch {
	case in.Ref != "" || len(in.Refs) > 0 || in.Sparql != "":
		return nil, refuse("argument", "SPEC §9.10", "query feed takes a cursor, not ref or sparql")
	case in.Filter.active():
		return nil, refuse("filter.invalid", "SPEC §9.10", "query feed takes no filter: it is every trusted record after the cursor")
	case in.Limit < 0 || in.Limit > sparqlMaxLimit:
		return nil, refuse("argument", "SPEC §9.10", "limit must be from 1 to %d, not %d", sparqlMaxLimit, in.Limit)
	}
	cur := map[string]feedPos{}
	if in.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil || json.Unmarshal(b, &cur) != nil {
			return nil, refuse("argument", "SPEC §9.10", "cursor is not one this cockpit handed out")
		}
	}
	limit := in.Limit
	if limit == 0 {
		limit = feedLimit
	}
	signer, err := verify.SignerOf(cfg)
	if err != nil {
		return nil, err
	}
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return nil, err
	}
	page := &FeedPage{Records: []FeedRecord{}}
	next := map[string]feedPos{}
	for k, v := range cur {
		next[k] = v
	}
	for _, input := range inputs {
		name := "own"
		if input.Name != binaries.Own {
			name = input.Name
		}
		for _, layer := range []struct{ kind, dir string }{{"foton", input.PlanktonDir}, {"claim", input.NektonDir}} {
			key := feedKey(layer.kind, layer.dir)
			pos, err := core.ReadPosition(layer.dir)
			if err != nil || pos.Epoch == "" {
				continue // no registry, or nothing numbered yet
			}
			from := cur[key]
			if from.Epoch != pos.Epoch {
				from = feedPos{Epoch: pos.Epoch}
			}
			if page.More || (cur[key].Epoch == pos.Epoch && cur[key].Objects == pos.Objects) {
				next[key] = cur[key]
				continue
			}
			recs, err := feed(layer.kind, layer.dir, from.Seq)
			if err != nil {
				return nil, err
			}
			whole := true
			for _, r := range recs {
				if len(page.Records) >= limit {
					page.More, whole = true, false
					break
				}
				from.Seq = r.seq
				keyid, tier, path := signer(r.env)
				if tier == verify.Untrusted {
					page.Untrusted++
					continue
				}
				env, _ := json.Marshal(r.env)
				page.Records = append(page.Records, FeedRecord{Kind: layer.kind, ID: r.id, Registry: name,
					Signer:   FeedSigner{KeyID: keyid, Tier: tier, Name: strings.TrimSuffix(filepath.Base(path), ".pub")},
					Envelope: env})
			}
			// The stamp goes into the cursor only once the registry was read to its end; a page that
			// stopped inside it has to be read on from there.
			if whole {
				from.Objects = pos.Objects
			}
			next[key] = from
		}
	}
	b, _ := json.Marshal(next)
	page.Cursor = base64.RawURLEncoding.EncodeToString(b)
	return &AskResult{Query: in.Query, Feed: page,
		FilterApplied: "none — every record after the cursor a key in your trust tiers verifies; the rest are counted"}, nil
}

type fedRecord struct {
	seq int
	id  string
	env core.Envelope
}

// feed is a registry's records after seq, in the kernel's order, read without writing to it.
func feed(kind, dir string, since int) ([]fedRecord, error) {
	var out []fedRecord
	if kind == "foton" {
		reg, err := binaries.OpenPlanktonSource(dir)
		if err != nil {
			return nil, err
		}
		for _, r := range reg.Records(since) {
			out = append(out, fedRecord{r.Seq, r.FotonID, r.Envelope})
		}
		return out, nil
	}
	reg, err := binaries.OpenNektonSource(dir)
	if err != nil {
		return nil, err
	}
	for _, r := range reg.Records(since) {
		out = append(out, fedRecord{r.Seq, r.ClaimID, r.Envelope})
	}
	return out, nil
}

// feedKey names a registry in the cursor: short, and the same for the same directory.
func feedKey(kind, dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	h := sha256.Sum256([]byte(kind + ":" + dir))
	return hex.EncodeToString(h[:6])
}
