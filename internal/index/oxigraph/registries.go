package oxigraph

// registries.go says which registries the store holds and which repositories read them (architecture
// view G10). What prepare notes here is observation, not record: no answer reads it, nothing under
// urn:kton:observed is touched, and a rebuild drops it with everything else.
//
// In urn:kton:index, beside what store.go writes:
//   - <urn:kton:reg:<id>> urn:kton:dir "<directory>";
//   - <urn:kton:reg:<id>> urn:kton:readFor <urn:kton:read:<h>>, one node per registry and repository
//     root, with urn:kton:root "<root>" and urn:kton:lastRead "<UTC minute>".

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
)

// Registry is one registry the store holds, as `index registries --json` lists it.
type Registry struct {
	ID       string          `json:"id"`
	Dir      string          `json:"dir,omitempty"` // absent until a query notes it read (after an upgrade, once)
	Records  int             `json:"records"`
	Keys     []string        `json:"keys"` // the keyids it was attributed under
	Position json.RawMessage `json:"position,omitempty"`
	Cursor   int             `json:"cursor"`
	// Stale says the registry moved since the store read it: the next query that reads it reads it
	// again. Absent when its directory is not known.
	Stale  *bool            `json:"stale,omitempty"`
	ReadBy []RegistryReader `json:"readBy"`
}

// RegistryReader is a repository whose queries read a registry, and the last minute one did.
type RegistryReader struct {
	Root     string `json:"root"`
	LastRead string `json:"lastRead"`
}

// readMinute is the precision of lastRead: a query writes a note only when its minute or its root
// changed, so warm queries within a minute write nothing.
func readMinute(t time.Time) string { return t.UTC().Truncate(time.Minute).Format("2006-01-02T15:04Z") }

func readIRI(reg, root string) string {
	h := sha256.Sum256([]byte(reg + "\x00" + root))
	return kNS + "read:" + hex.EncodeToString(h[:12])
}

// notes counts the read notes written, for a test that a warm query writes none.
var notes atomic.Int64

// noteRead records that root read slot's registries at now's minute. It writes only when the marker
// for slot and root names another minute, and only for a daemon whose bookkeeping this machine keeps
// (d.Dir). A failure is the caller's to ignore: an observation never fails a query.
func (d *Daemon) noteRead(ctx context.Context, slot, root string, inputs []binaries.Input, now time.Time) error {
	if d.Dir == "" || root == "" {
		return nil
	}
	minute := readMinute(now)
	h := sha256.Sum256([]byte(root))
	marker := filepath.Join(d.datasetDir(), "readfor", slot+"-"+hex.EncodeToString(h[:8]))
	if b, err := os.ReadFile(marker); err == nil && string(b) == minute {
		return nil
	}
	var del, ins strings.Builder
	for _, in := range inputs {
		for _, dir := range []string{in.PlanktonDir, in.NektonDir} {
			abs, err := filepath.Abs(dir)
			if err != nil {
				abs = dir
			}
			reg, node := regIRI(dir), readIRI(regIRI(dir), root)
			fmt.Fprintf(&del, "DELETE WHERE { GRAPH <%s> { <%s> <%slastRead> ?t } } ;\n", indexGraph, node, kNS)
			fmt.Fprintf(&ins, "<%s> <%sdir> %s .\n<%s> <%sreadFor> <%s> .\n<%s> <%sroot> %s .\n<%s> <%slastRead> %s .\n",
				reg, kNS, nqLit(abs), reg, kNS, node, node, kNS, nqLit(root), node, kNS, nqLit(minute))
		}
	}
	if ins.Len() == 0 {
		return nil
	}
	update := del.String() + fmt.Sprintf("INSERT DATA { GRAPH <%s> { %s } }", indexGraph, ins.String())
	if _, err := d.post(ctx, "/update", "", url.Values{"update": {update}}); err != nil {
		return err
	}
	notes.Add(1)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(minute), 0o644)
}

// RegistryList lists every registry the store holds: what it read, how current that is against the
// registry now, and which repositories' queries read it. It only reads.
func (d *Daemon) RegistryList(ctx context.Context) ([]Registry, error) {
	by := map[string]*Registry{}
	get := func(id string) *Registry {
		if !strings.HasPrefix(id, kNS+"reg:") {
			return nil
		}
		r := by[id]
		if r == nil {
			r = &Registry{ID: id, Keys: []string{}, ReadBy: []RegistryReader{}}
			by[id] = r
		}
		return r
	}
	q := fmt.Sprintf(`SELECT ?r ?p ?o WHERE { GRAPH <%[1]s> { ?r ?p ?o FILTER(?p IN (<%[2]sposition>, <%[2]scursor>, <%[2]skey>, <%[2]sdir>)) } }`, indexGraph, kNS)
	rows, err := d.selectCSV(ctx, url.Values{"query": {q}})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		r := get(row["r"])
		if r == nil {
			continue
		}
		switch row["p"] {
		case kNS + "position":
			r.Position = json.RawMessage(row["o"])
		case kNS + "cursor":
			r.Cursor, _ = strconv.Atoi(row["o"])
		case kNS + "dir":
			r.Dir = row["o"]
		case kNS + "key":
			if b, err := hex.DecodeString(row["o"]); err == nil && len(b) == ed25519.PublicKeySize {
				r.Keys = append(r.Keys, core.KeyIDHex(ed25519.PublicKey(b)))
			}
		}
	}
	q = fmt.Sprintf(`SELECT ?r (COUNT(?g) AS ?n) WHERE { GRAPH <%s> { ?r <%sholds> ?g } } GROUP BY ?r`, indexGraph, kNS)
	if rows, err = d.selectCSV(ctx, url.Values{"query": {q}}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if r := get(row["r"]); r != nil {
			r.Records, _ = strconv.Atoi(row["n"])
		}
	}
	q = fmt.Sprintf(`SELECT ?r ?root ?at WHERE { GRAPH <%[1]s> { ?r <%[2]sreadFor> ?n . ?n <%[2]sroot> ?root ; <%[2]slastRead> ?at } }`, indexGraph, kNS)
	if rows, err = d.selectCSV(ctx, url.Values{"query": {q}}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if r := get(row["r"]); r != nil {
			r.ReadBy = append(r.ReadBy, RegistryReader{Root: row["root"], LastRead: row["at"]})
		}
	}

	out := make([]Registry, 0, len(by))
	for _, r := range by {
		sort.Strings(r.Keys)
		sort.Slice(r.ReadBy, func(i, j int) bool { return r.ReadBy[i].Root < r.ReadBy[j].Root })
		if r.Dir != "" {
			stale := true
			if pos, err := core.ReadPosition(r.Dir); err == nil {
				b, _ := json.Marshal(pos)
				stale = string(b) != string(r.Position)
			}
			r.Stale = &stale
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir < out[j].Dir
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
