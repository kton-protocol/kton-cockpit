package oxigraph

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
)

// A query's dataset (the graphs a configuration's trusted keys may see) changes only when a
// registry moves or the keys change. It is kept on disk under exactly that, so a query against
// registries that did not move is the query alone: no reading in, no asking the store what it holds.
// The positions are read from the registries' own files, which is what reading in compares too.

// A query's default graph is one view graph per configuration, not the records' graphs listed one
// by one: the engine joins over a union of a few thousand default graphs graph by graph, which on a
// union of 24 registries made a four-pattern join run for minutes instead of a second. The view is
// the union copied once, when the dataset is computed, and marked with the dataset's key; a kept
// dataset is used only while its view still carries that mark.

// datasetKey names the configuration (slot: its registries and trusted keys, wherever they stand)
// and the dataset of these registries, at their current positions, under keys (key). The key is ""
// when a position cannot be read, and then nothing is kept.
func datasetKey(inputs []binaries.Input, keys []ed25519.PublicKey) (slot, key string) {
	var regs, parts []string
	unread := false
	for _, in := range inputs {
		for _, dir := range []string{in.PlanktonDir, in.NektonDir} {
			regs = append(regs, regIRI(dir))
			pos, err := core.ReadPosition(dir)
			if err != nil {
				unread = true
				continue
			}
			b, _ := json.Marshal(pos)
			parts = append(parts, regIRI(dir)+"="+string(b))
		}
	}
	ids := make([]string, 0, len(keys))
	for k := range trustedIDs(keys) {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	slot = digest(append(regs, ids...))
	if unread {
		return slot, ""
	}
	return slot, digest(append(parts, ids...))
}

func digest(parts []string) string {
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:16])
}

func viewIRI(slot string) string { return kNS + "view:" + slot }
func markIRI(slot string) string { return kNS + "view-of:" + slot }

func (d *Daemon) datasetDir() string { return filepath.Join(d.Dir, "datasets") }

type keptFile struct {
	Key        string
	Named      []string
	Registries int
}

func (d *Daemon) keptDataset(ctx context.Context, slot, key string) (*prepared, bool) {
	if d.Dir == "" || key == "" {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(d.datasetDir(), slot+".json"))
	if err != nil {
		return nil, false
	}
	var k keptFile
	if json.Unmarshal(b, &k) != nil || k.Key != key || !d.viewIs(ctx, slot, key) {
		return nil, false
	}
	return &prepared{def: []string{viewIRI(slot)}, named: k.Named, registries: k.Registries, kept: true}, true
}

// viewIs tells whether slot's view holds the dataset key names; another process may have replaced it.
func (d *Daemon) viewIs(ctx context.Context, slot, key string) bool {
	q := "ASK { GRAPH <" + markIRI(slot) + "> { <" + viewIRI(slot) + "> <" + kNS + "key> " + strconv.Quote(key) + " } }"
	body, err := d.post(ctx, "/query", "application/sparql-results+json", url.Values{"query": {q}})
	if err != nil {
		return false
	}
	r, err := ParseResults(body)
	return err == nil && r.Boolean != nil && *r.Boolean
}

// makeView copies the graphs of def into slot's view and marks it with key, in one update, so a
// reader sees either the old view and mark or the new ones.
func (d *Daemon) makeView(ctx context.Context, slot, key string, def []string) error {
	v, m := "<"+viewIRI(slot)+">", "<"+markIRI(slot)+">"
	var u strings.Builder
	u.WriteString("DROP SILENT GRAPH " + v + " ;\nDROP SILENT GRAPH " + m + " ;\n")
	if len(def) > 0 {
		u.WriteString("INSERT { GRAPH " + v + " { ?s ?p ?o } } WHERE { VALUES ?g {")
		for _, g := range def {
			u.WriteString(" <" + g + ">")
		}
		u.WriteString(" } GRAPH ?g { ?s ?p ?o } } ;\n")
	}
	u.WriteString("INSERT DATA { GRAPH " + m + " { " + v + " <" + kNS + "key> " + strconv.Quote(key) + " } }")
	_, err := d.post(ctx, "/update", "", url.Values{"update": {u.String()}})
	return err
}

func (d *Daemon) keepDataset(slot, key string, p *prepared) {
	if d.Dir == "" || key == "" {
		return
	}
	b, _ := json.Marshal(keptFile{key, p.named, p.registries})
	if os.MkdirAll(d.datasetDir(), 0o755) != nil {
		return
	}
	tmp := filepath.Join(d.datasetDir(), slot+"."+strconv.Itoa(os.Getpid())+".tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, filepath.Join(d.datasetDir(), slot+".json"))
	}
}

// forgetDatasets drops every kept dataset: the store they describe was emptied.
func (d *Daemon) forgetDatasets() {
	if d.Dir != "" {
		os.RemoveAll(d.datasetDir())
	}
}
