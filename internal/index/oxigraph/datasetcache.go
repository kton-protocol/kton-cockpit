package oxigraph

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
)

// A query's dataset (the graphs a configuration's trusted keys may see) changes only when a
// registry moves or the keys change. It is kept on disk under exactly that, so a query against
// registries that did not move is the query alone: no reading in, no asking the store what it holds.
// The positions are read from the registries' own files, which is what reading in compares too.

// datasetKey names the dataset of these registries, at their current positions, under keys. It is
// "" when a position cannot be read, and then nothing is kept.
func datasetKey(inputs []binaries.Input, keys []ed25519.PublicKey) string {
	var parts []string
	for _, in := range inputs {
		for _, dir := range []string{in.PlanktonDir, in.NektonDir} {
			pos, err := core.ReadPosition(dir)
			if err != nil {
				return ""
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
	parts = append(parts, ids...)
	b, _ := json.Marshal(parts)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:16])
}

func (d *Daemon) datasetDir() string { return filepath.Join(d.Dir, "datasets") }

func (d *Daemon) keptDataset(key string) (*prepared, bool) {
	if d.Dir == "" || key == "" {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(d.datasetDir(), key+".json"))
	if err != nil {
		return nil, false
	}
	var k struct {
		Def, Named []string
		Registries int
	}
	if json.Unmarshal(b, &k) != nil {
		return nil, false
	}
	return &prepared{def: k.Def, named: k.Named, registries: k.Registries, kept: true}, true
}

func (d *Daemon) keepDataset(key string, p *prepared) {
	if d.Dir == "" || key == "" {
		return
	}
	b, _ := json.Marshal(struct {
		Def, Named []string
		Registries int
	}{p.def, p.named, p.registries})
	if os.MkdirAll(d.datasetDir(), 0o755) != nil {
		return
	}
	tmp := filepath.Join(d.datasetDir(), key+".tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, filepath.Join(d.datasetDir(), key+".json"))
	}
}

// forgetDatasets drops every kept dataset: the store they describe was emptied.
func (d *Daemon) forgetDatasets() {
	if d.Dir != "" {
		os.RemoveAll(d.datasetDir())
	}
}
