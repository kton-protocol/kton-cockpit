package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/flock"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
)

// Loading derived RDF (ADR-017, SPEC §12.6): `cockpit load` puts a file's RDF into the index as a graph
// named by the file's hash, for this repository's queries. It is an operator command, not a verb: it
// signs nothing and records nothing outside the index, and a rebuild of the index drops what it loaded.

// LoadRequest names the files to load: repository paths, or sha256:<hex> of bytes in the repository's
// content store (.kton/blobs). As is the media type to read them as; empty takes it from each file's
// extension.
type LoadRequest struct {
	Files []string `json:"files"`
	As    string   `json:"as,omitempty"`
	// Label names the loaded graphs for a query (rdfs:label on their description, ADR-018 section 4a).
	Label string `json:"label,omitempty"`
}

// LoadResult is what was loaded, in the order asked.
type LoadResult struct {
	Loaded []oxigraph.LoadedGraph `json:"loaded"`
}

// loadTypes are the media types a load reads, by the extension that names them.
var loadTypes = map[string]string{
	".ttl":    "text/turtle",
	".nt":     "application/n-triples",
	".rdf":    "application/rdf+xml",
	".owl":    "application/rdf+xml",
	".jsonld": "application/ld+json",
}

// quadTypes carry graph names of their own, which would compete with the hash as the graph's name.
var quadTypes = map[string]bool{"application/n-quads": true, "application/trig": true}

func mediaTypeFor(as, name string) (string, error) {
	if as != "" {
		if quadTypes[as] {
			return "", refuse("derived.quads", "SPEC §12.6", "%s carries graph names of its own; a load names the graph by the file's hash", as)
		}
		for _, t := range loadTypes {
			if t == as {
				return as, nil
			}
		}
		return "", refuse("derived.type", "SPEC §12.6", "%q is not a type a load reads (text/turtle, application/n-triples, application/rdf+xml, application/ld+json)", as)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if t, ok := loadTypes[ext]; ok {
		return t, nil
	}
	return "", refuse("derived.type", "SPEC §12.6", "%s: say what it is with --as; the extension %q names no type a load reads (.ttl, .nt, .rdf, .owl, .jsonld)", name, ext)
}

// loadIndex is the running index a load writes into, refused unless the configuration has one and
// lets loads into it.
func loadIndex(ctx context.Context, cfg *config.Config) (*oxigraph.Daemon, error) {
	if cfg.Raw.Index.Kind != "oxigraph" {
		return nil, refuse("index.not-configured", "SPEC §12.6", "a load goes into the index: set index.kind to oxigraph in cockpit.config.json")
	}
	if !cfg.Raw.Index.Derived.Enabled {
		return nil, refuse("derived.disabled", "SPEC §12.6", "loading is off: set index.derived.enabled in cockpit.config.json")
	}
	d, err := oxigraph.Ensure(ctx, cfg)
	if err != nil {
		return nil, refuse("index", "SPEC §12.6", "%v", err)
	}
	return d, nil
}

// Load reads each file, checks its bytes against its hash and the limits, and loads it into the
// index. Every file is read and checked before any is loaded.
func (c *Cockpit) Load(ctx context.Context, in LoadRequest) (*LoadResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	if len(in.Files) == 0 {
		return nil, refuse("argument", "SPEC §12.6", "load needs at least one file or sha256")
	}
	type file struct {
		ref, hex, mediaType string
		data                []byte
	}
	run := binaries.New(cfg)
	var files []file
	for _, ref := range in.Files {
		b, hex, err := loadBytes(cfg, ref)
		if err != nil {
			return nil, err
		}
		mt, err := mediaTypeFor(in.As, ref)
		if err != nil {
			return nil, err
		}
		// A file whose hash is a record id of the union would take that record's graph name; its graph may
		// not be in the index yet, so the registries are asked (kton-review L1 on #192).
		if _, err := run.EnvelopeFor(ctx, "sha256:"+hex); err == nil {
			return nil, refuse("derived.clash", "SPEC §12.6", "%s hashes to sha256:%s, the id of a record of this repository: a record's graph has that name", ref, hex)
		}
		// A DTD's nested entities expand without bound while the engine parses (kton-review on ADR-017).
		if mt == "application/rdf+xml" && bytes.Contains(b, []byte("<!ENTITY")) {
			return nil, refuse("derived.refused", "SPEC §12.6", "%s declares XML entities; a load does not read RDF/XML with a DTD", ref)
		}
		files = append(files, file{ref, hex, mt, b})
	}
	d, err := loadIndex(ctx, cfg)
	if err != nil {
		return nil, err
	}
	unlock, err := lockLoads(cfg)
	if err != nil {
		return nil, refuse("io", "SPEC §12.6", "%v", err)
	}
	defer unlock()
	out := &LoadResult{Loaded: []oxigraph.LoadedGraph{}}
	for _, f := range files {
		// The graph's name is the hash of exactly these bytes, the ones sent to the engine.
		g, err := d.LoadGraph(ctx, cfg, f.hex, f.data, f.mediaType, f.ref, describe(ctx, run, f.hex, in.Label))
		if errors.Is(err, oxigraph.ErrClash) {
			return nil, refuse("derived.clash", "SPEC §12.6", "%s: %v", f.ref, err)
		} else if errors.Is(err, oxigraph.ErrLoadRefused) {
			return nil, refuse("derived.refused", "SPEC §12.6", "%s: %v", f.ref, err)
		} else if err != nil {
			return nil, refuse("index", "SPEC §12.6", "%s: %v", f.ref, err)
		}
		out.Loaded = append(out.Loaded, *g)
		// Remembered by the checkout, unsigned and never shared, so the index loads it again after a
		// rebuild (ADR-017, section 9).
		if err := rememberLoad(cfg, loadEntry{Hash: g.Hash, From: f.ref, MediaType: f.mediaType, Label: in.Label}); err != nil {
			return nil, refuse("io", "SPEC §12.6", "%v", err)
		}
	}
	return out, nil
}

// loadEntry is one line of the checkout's load list.
type loadEntry struct {
	Hash      string `json:"hash"`
	From      string `json:"from"`
	MediaType string `json:"mediaType"`
	Label     string `json:"label,omitempty"`
}

// describe is how a loaded file's graph is found (ADR-018, section 4a): the output paths the steps
// that produced it record for it, and the label the load gave it. A file no step produced has none.
// Only steps a key of the trust tiers signed name a path: an untrusted step writing the same bytes
// elsewhere would otherwise name this repository's graph (kton-review F1 on #208).
func describe(ctx context.Context, run *binaries.Runner, hex, label string) oxigraph.Description {
	desc := oxigraph.Description{Label: label}
	prod, err := run.Producer(ctx, "sha256:"+hex)
	if err != nil {
		return desc
	}
	seen := map[string]bool{}
	for _, rec := range prod.Records {
		f, err := run.FotonByID(ctx, rec.ID)
		if err != nil || f.SignerKeyID == "" {
			continue
		}
		for _, o := range f.Outputs {
			if strings.EqualFold(o.Hash, "sha256:"+hex) && o.Path != "" && !seen[o.Path] {
				seen[o.Path] = true
				desc.Paths = append(desc.Paths, o.Path)
			}
		}
	}
	sort.Strings(desc.Paths)
	return desc
}

func loadListPath(cfg *config.Config) string { return filepath.Join(cfg.LocalDir, "loaded.json") }

func readLoadList(cfg *config.Config) ([]loadEntry, error) {
	b, err := os.ReadFile(loadListPath(cfg))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var l struct {
		Loaded []loadEntry `json:"loaded"`
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", loadListPath(cfg), err)
	}
	return l.Loaded, nil
}

func writeLoadList(cfg *config.Config, l []loadEntry) error {
	b, err := json.MarshalIndent(struct {
		Loaded []loadEntry `json:"loaded"`
	}{l}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.LocalDir, 0o755); err != nil {
		return err
	}
	tmp := loadListPath(cfg) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, loadListPath(cfg))
}

func rememberLoad(cfg *config.Config, e loadEntry) error {
	l, err := readLoadList(cfg)
	if err != nil {
		return err
	}
	for i := range l {
		if l[i].Hash == e.Hash {
			l[i] = e
			return writeLoadList(cfg, l)
		}
	}
	return writeLoadList(cfg, append(l, e))
}

func forgetLoad(cfg *config.Config, hash string) error {
	l, err := readLoadList(cfg)
	if err != nil {
		return err
	}
	kept := l[:0]
	for _, e := range l {
		if e.Hash != hash {
			kept = append(kept, e)
		}
	}
	return writeLoadList(cfg, kept)
}

// lockLoads serialises what writes this repository's loads: a load, a drop, and a reload after a
// rebuild, across processes (kton-review on ADR-017).
func lockLoads(cfg *config.Config) (func(), error) {
	return flock.Lock(flock.InCheckout(cfg.RepoRoot, "load"))
}

// reloadMissing loads again what the checkout's load list names and the index's catalogue lacks: after
// a rebuild, a schema change or a cleared cache. Each file is read again and checked against its hash;
// one that is gone or changed is not loaded and is counted (ADR-017, section 9).
func reloadMissing(ctx context.Context, cfg *config.Config, d *oxigraph.Daemon) (missing int, err error) {
	l, err := readLoadList(cfg)
	if err != nil || len(l) == 0 {
		return 0, err
	}
	unlock, err := lockLoads(cfg)
	if err != nil {
		return 0, err
	}
	defer unlock()
	have, err := d.Loaded(ctx, cfg.RepoRoot)
	if err != nil {
		return 0, err
	}
	in := map[string]bool{}
	for _, g := range have {
		in[g.Hash] = true
	}
	for _, e := range l {
		if in[e.Hash] {
			continue
		}
		b, hex, err := loadBytes(cfg, e.From)
		if err != nil || "sha256:"+hex != e.Hash {
			missing++
			continue
		}
		if _, err := d.LoadGraph(ctx, cfg, hex, b, e.MediaType, e.From, describe(ctx, binaries.New(cfg), hex, e.Label)); err != nil {
			missing++
		}
	}
	return missing, nil
}

// LoadList is what is loaded for this repository, and what the checkout's load list names that could
// not be loaded again.
type LoadList struct {
	Loaded  []oxigraph.LoadedGraph `json:"loaded"`
	Missing []LoadMissing          `json:"missing"`
}

// LoadMissing is a hash on the load list the index does not hold, and why.
type LoadMissing struct {
	Hash string `json:"hash"`
	From string `json:"from"`
	Why  string `json:"why"`
}

// LoadedGraphs lists what was loaded for this repository, after loading again what the load list
// names and the index lacks.
func (c *Cockpit) LoadedGraphs(ctx context.Context) (*LoadList, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	d, err := loadIndex(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := reloadMissing(ctx, cfg, d); err != nil {
		return nil, refuse("index", "SPEC §12.6", "%v", err)
	}
	l, err := d.Loaded(ctx, cfg.RepoRoot)
	if err != nil {
		return nil, refuse("index", "SPEC §12.6", "%v", err)
	}
	out := &LoadList{Loaded: l, Missing: []LoadMissing{}}
	have := map[string]bool{}
	for _, g := range l {
		have[g.Hash] = true
	}
	list, err := readLoadList(cfg)
	if err != nil {
		return nil, refuse("io", "SPEC §12.6", "%v", err)
	}
	for _, e := range list {
		if have[e.Hash] {
			continue
		}
		why := "it could not be read as " + e.MediaType
		if _, hex, err := loadBytes(cfg, e.From); err != nil {
			why = err.Error()
		} else if "sha256:"+hex != e.Hash {
			why = "its bytes changed (now sha256:" + hex + ")"
		}
		out.Missing = append(out.Missing, LoadMissing{Hash: e.Hash, From: e.From, Why: why})
	}
	return out, nil
}

// DropLoaded takes a loaded graph out of this repository's queries.
func (c *Cockpit) DropLoaded(ctx context.Context, ref string) error {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return refuse("binding", "SPEC §5", "%v", err)
	}
	h, ok := core.NormalizeContentHash(ref)
	if !ok {
		return refuse("argument", "SPEC §12.6", "drop takes the sha256 of a loaded file, not %q", ref)
	}
	d, err := loadIndex(ctx, cfg)
	if err != nil {
		return err
	}
	unlock, err := lockLoads(cfg)
	if err != nil {
		return refuse("io", "SPEC §12.6", "%v", err)
	}
	defer unlock()
	listed, err := readLoadList(cfg)
	if err != nil {
		return refuse("io", "SPEC §12.6", "%v", err)
	}
	onList := false
	for _, e := range listed {
		onList = onList || e.Hash == h
	}
	had, err := d.DropLoaded(ctx, cfg.RepoRoot, strings.TrimPrefix(h, "sha256:"))
	if err != nil {
		return refuse("index", "SPEC §12.6", "%v", err)
	}
	if !had && !onList {
		return refuse("argument", "SPEC §12.6", "%s is not loaded for this repository", h)
	}
	if err := forgetLoad(cfg, h); err != nil {
		return refuse("io", "SPEC §12.6", "%v", err)
	}
	return nil
}

// loadBytes reads a load's file: a path inside the repository, or a hash in its content store. It
// refuses what leaves the repository, a signing key, a file past index.derived.maxBytes, and a hash
// the store does not hold.
func loadBytes(cfg *config.Config, ref string) ([]byte, string, error) {
	limit := cfg.Raw.Index.Derived.MaxBytesOrDefault()
	var path string
	if h, ok := core.NormalizeContentHash(ref); ok && strings.HasPrefix(ref, "sha256:") {
		hex := strings.TrimPrefix(h, "sha256:")
		path = filepath.Join(cfg.BlobsDir, hex[:2], hex)
		if _, err := os.Stat(path); err != nil {
			return nil, "", refuse("derived.missing", "SPEC §12.6", "%s is not in this repository's content store (%s)", h, config.Rel("blobs"))
		}
	} else {
		root, err := filepath.Abs(cfg.RepoRoot)
		if err != nil {
			return nil, "", err
		}
		path = filepath.Clean(filepath.Join(root, ref))
		if filepath.IsAbs(ref) {
			path = filepath.Clean(ref)
		}
		// Checked where the path leads, not as written: a symlink inside the repository may point
		// anywhere (kton-review L2 on #192).
		if real, err := filepath.EvalSymlinks(root); err == nil {
			root = real
		}
		if real, err := filepath.EvalSymlinks(path); err == nil {
			path = real
		}
		if rel, err := filepath.Rel(root, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, "", refuse("path.denied", "SPEC §12.6", "%s is outside this repository", ref)
		}
		if strings.HasSuffix(path, ".key") || strings.Contains(filepath.ToSlash(path)+"/", "/.kton/local/") {
			return nil, "", refuse("path.denied", "SPEC §12.6", "%s is not a file a load reads", ref)
		}
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, "", refuse("derived.missing", "SPEC §12.6", "%s: %v", ref, err)
	}
	if st.Size() > limit {
		return nil, "", refuse("derived.too-large", "SPEC §12.6", "%s is %d bytes, more than index.derived.maxBytes (%d)", ref, st.Size(), limit)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", refuse("derived.missing", "SPEC §12.6", "%s: %v", ref, err)
	}
	hex := strings.TrimPrefix(core.HashBytes(b), "sha256:")
	if strings.HasPrefix(ref, "sha256:") && "sha256:"+hex != strings.ToLower(ref) {
		return nil, "", refuse("derived.mismatch", "SPEC §12.6", "the store's bytes for %s hash to sha256:%s", ref, hex)
	}
	return b, hex, nil
}
