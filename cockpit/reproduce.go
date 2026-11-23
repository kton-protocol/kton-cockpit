package cockpit

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kton.dev/plankton/blobstore"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// Reproducing a query run (ADR-020, section 4): its manifest read from the content store, its dataset
// built again from the registries, never from stored rows, truncated at the pinned heads, evaluated with
// the pinned trust, view and engine, and the canonical result compared with the run's. A match is
// recorded as the same foton, signed by the reproducer, so the run's ↻N counts it.

// ReproduceRequest names the query run to reproduce.
type ReproduceRequest struct {
	Run            string `json:"run"` // the query run's foton id
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	// SignAnyway signs a match that rests on keys this repository does not trust: the reproducer then
	// vouches for a dataset whose signers it does not know, and says so by asking for it.
	SignAnyway bool `json:"signAnyway,omitempty"`
}

// ReproduceResult is a match: the record it signed, and what differs from the reproducer's present
// beside it, not counted against it (ADR-020 A3).
type ReproduceResult struct {
	Run string `json:"run"`
	// Reproducible: the pinned dataset, rebuilt from the registries and evaluated with the pinned view and
	// engine, gives the run's result (ADR-020 A3: apart from whether it is still accepted here).
	Reproducible bool `json:"reproducible"`
	// Signed: this repository signed the run's record. Only when a key of this repository's tiers signed
	// the run and every key the run's trust pinned is in them, or SignAnyway was asked: a co-signature counts in the run's ↻N as an
	// independent reproduction by a key others may trust, and carries no notes (kton-review R1 on #229).
	Signed bool `json:"signed"`
	// NotSigned says why a reproducible run was not signed.
	NotSigned string `json:"notSigned,omitempty"`
	FotonID   string `json:"fotonId,omitempty"` // the same as Run when signed: a reproduction signs the run's own record
	Result    string `json:"result"`            // sha256 of the canonical result, the run's and this one's
	Rows      int    `json:"rows"`
	Revision  string `json:"revision,omitempty"`
	// Notes say where the reproducer's present differs from the run's pins: a pinned key it no longer
	// trusts, another vocabulary, other packages.
	Notes []string `json:"notes,omitempty"`
}

// Reproduce reproduces the query run in.Run and, when its result matches, signs it.
func (c *Cockpit) Reproduce(ctx context.Context, in ReproduceRequest) (*ReproduceResult, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	if in.Run == "" {
		return nil, refuse("argument", "ADR-020", "reproduce needs the query run's id")
	}
	timeout := time.Duration(in.TimeoutSeconds) * time.Second
	if in.TimeoutSeconds < 0 || timeout > sparqlMaxTimeout {
		return nil, refuse("argument", "ADR-020", "timeoutSeconds must be from 1 to %d, not %d", int(sparqlMaxTimeout/time.Second), in.TimeoutSeconds)
	}
	if timeout == 0 {
		timeout = sparqlTimeout
	}
	f, err := binaries.New(cfg).Foton(ctx, in.Run)
	if err != nil {
		return nil, refuse("reproduce.missing", "ADR-020", "%v", err)
	}
	if f.Protocol.Kind != "query/sparql" {
		return nil, refuse("argument", "ADR-020", "%s is a %q record, not a query run", in.Run, f.Protocol.Kind)
	}
	refs := map[string]string{}
	for _, r := range append(append([]core.FileRef{}, f.Inputs...), f.Outputs...) {
		refs[r.Path] = r.Hash
	}
	for _, p := range []string{"query.rq", "manifest.json", "result.json"} {
		if refs[p] == "" {
			return nil, refuse("argument", "ADR-020", "%s names no %s: not a query run this cockpit reads", in.Run, p)
		}
	}
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return nil, refuse("kernel", "", "%v", err)
	}
	blob := func(what, h string) ([]byte, error) {
		if b, ok := findBlob(cfg, inputs, h); ok {
			return b, nil
		}
		return nil, refuse("reproduce.missing", "ADR-020", "the %s pinned as %s is in no content store this repository reads", what, h)
	}
	q, err := blob("query", refs["query.rq"])
	if err != nil {
		return nil, err
	}
	mb, err := blob("manifest", refs["manifest.json"])
	if err != nil {
		return nil, err
	}
	var m QueryManifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, refuse("reproduce.unsupported", "ADR-020", "the manifest %s is not one this cockpit reads: %v", refs["manifest.json"], err)
	}
	if m.Version != manifestVersion {
		return nil, refuse("reproduce.unsupported", "ADR-020", "the manifest is version %d; this cockpit reproduces version %d (version 1 does not pin the view's signers, row templates and package aliases)", m.Version, manifestVersion)
	}
	if m.ViewVersion != oxigraph.ViewVersion() {
		return nil, refuse("reproduce.unsupported", "ADR-020", "the run was projected with view version %d; this cockpit projects version %d", m.ViewVersion, oxigraph.ViewVersion())
	}
	if d, ok := f.Protocol.Descriptor["engine"].(string); !ok || d != m.Engine {
		return nil, refuse("reproduce.mismatch", "ADR-020", "the record's engine %v is not the manifest's %s", f.Protocol.Descriptor["engine"], m.Engine)
	}
	// The engine is pinned by digest and never pulled: absent, the reproduction is refused, named.
	if !enginePresent(ctx, cfg, m.Engine) {
		return nil, refuse("reproduce.unsupported", "ADR-020", "the engine %s is not on this machine; it is never pulled for a reproduction: pull it by its digest first", m.Engine)
	}
	vb, err := blob("view", m.View)
	if err != nil {
		return nil, err
	}
	var view pinnedView
	if err := json.Unmarshal(vb, &view); err != nil {
		return nil, refuse("reproduce.unsupported", "ADR-020", "the pinned view %s: %v", m.View, err)
	}
	pinned := &pinnedSet{inputs: inputs, ids: map[string]bool{}, sources: map[string][]string{}, view: &view}
	for _, r := range m.Registries {
		for _, l := range []struct{ what, h string }{{"records", r.Records}, {"claims", r.Claims}} {
			b, err := blob("list of "+r.Name+"'s "+l.what, l.h)
			if err != nil {
				return nil, err
			}
			for _, id := range strings.Split(string(b), "\n") {
				if id == "" {
					continue
				}
				pinned.ids[id] = true
				if l.what == "claims" {
					h := strings.ToLower(strings.TrimPrefix(id, "sha256:"))
					if s := pinned.sources[h]; len(s) == 0 || s[len(s)-1] != r.Name {
						pinned.sources[h] = append(s, r.Name)
					}
				}
			}
		}
	}
	for _, l := range m.Loaded {
		b, err := blob("loaded graph", l.Hash)
		if err != nil {
			return nil, err
		}
		pinned.loaded = append(pinned.loaded, loadedBytes{hex: strings.TrimPrefix(l.Hash, "sha256:"), mediaType: l.MediaType, data: b})
	}
	res, result, err := evaluatePinned(ctx, cfg, string(q), &m, pinned, timeout)
	if err != nil {
		return nil, err
	}
	if got := core.HashBytes(result); got != refs["result.json"] {
		return nil, refuse("reproduce.mismatch", "ADR-020", "every pinned input was found as pinned, and the result differs: %s", resultDiff(func() []byte {
			b, _ := blob("result", refs["result.json"])
			return b
		}(), result))
	}
	out := &ReproduceResult{Run: in.Run, Reproducible: true, Result: refs["result.json"], Rows: len(res.Terms), Notes: presentDiffers(cfg, &m)}
	// Signed only by a reproducer that knows who made the run and every key it rests on: steps are in the
	// dataset whoever signed them, so trusted pins alone do not make an untrusted run ours to vouch for
	// (kton-review's note on #229, MH 23.11.).
	var why []string
	signerTrusted := false
	if keys, err := binaries.TrustKeys(cfg, ""); err == nil {
		if env, ok := runEnvelope(ctx, cfg, in.Run); ok && core.VerifiedSignerKeyID(env, keys) != "" {
			signerTrusted = true
		}
	}
	if !signerTrusted {
		out.Notes = append(out.Notes, "the run is signed by no key trusted here")
		why = append(why, "the run is signed by no key trusted here")
	}
	if untrusted := untrustedPins(cfg, &m); len(untrusted) > 0 {
		why = append(why, fmt.Sprintf("it rests on %d key(s) not trusted here (%s)", len(untrusted), strings.Join(untrusted, ", ")))
	}
	if len(why) > 0 && !in.SignAnyway {
		out.NotSigned = strings.Join(why, "; ") + ": reproducible, not signed; sign-anyway signs it"
		return out, nil
	}
	store, err := blobstore.Open(cfg.BlobsDir)
	if err != nil {
		return nil, refuse("io", "", "%v", err)
	}
	// Every byte the manifest names goes into this repository's own store, so a reproduction from here
	// does not depend on the source it was found in.
	for _, h := range append([]string{m.View}, registryLists(&m)...) {
		if b, ok := findBlob(cfg, inputs, h); ok {
			if _, err := store.Put(b); err != nil {
				return nil, refuse("io", "", "%v", err)
			}
		}
	}
	run, err := recordQueryRun(ctx, cfg, store, q, mb, result, f.Protocol.Descriptor, "reproduction")
	if err != nil {
		return nil, err
	}
	out.Signed, out.FotonID, out.Revision = true, run.FotonID, run.Revision
	return out, nil
}

func registryLists(m *QueryManifest) []string {
	var out []string
	for _, r := range m.Registries {
		out = append(out, r.Records, r.Claims)
	}
	return out
}

// findBlob is the bytes of h from this repository's content store, or from a source's.
func findBlob(cfg *config.Config, inputs []binaries.Input, h string) ([]byte, bool) {
	dirs := []string{cfg.BlobsDir}
	for _, in := range inputs {
		if in.Name != binaries.Own && in.PlanktonDir != "" {
			dirs = append(dirs, filepath.Join(filepath.Dir(in.PlanktonDir), blobstore.Subdir))
		}
	}
	for _, d := range dirs {
		if _, err := os.Stat(d); err != nil {
			continue // never created in a source's copy
		}
		s, err := blobstore.Open(d)
		if err != nil || !s.Has(h) {
			continue
		}
		if b, err := s.Get(h); err == nil && core.HashBytes(b) == h {
			return b, true
		}
	}
	return nil, false
}

// enginePresent says whether the engine's image is on this machine, without pulling it; a test replaces it.
var enginePresent = imagePresent

func imagePresent(ctx context.Context, cfg *config.Config, image string) bool {
	ref := strings.TrimPrefix(image, "oci://")
	return exec.CommandContext(ctx, cfg.Raw.Execution.EngineOrDefault(), "image", "inspect", ref).Run() == nil
}

// resultDiff names how two canonical results differ: their row counts and the first row in one and not
// the other.
func resultDiff(was, now []byte) string {
	type canon struct {
		Rows    []string `json:"rows"`
		Boolean *bool    `json:"boolean"`
	}
	var a, b canon
	if json.Unmarshal(was, &a) != nil || json.Unmarshal(now, &b) != nil {
		return "the run's result cannot be read"
	}
	if a.Boolean != nil || b.Boolean != nil {
		return fmt.Sprintf("the run answered %v, the reproduction %v", deref(a.Boolean), deref(b.Boolean))
	}
	in := func(rows []string) map[string]int {
		m := map[string]int{}
		for _, r := range rows {
			m[r]++
		}
		return m
	}
	ma, mb := in(a.Rows), in(b.Rows)
	msg := fmt.Sprintf("the run has %d rows, the reproduction %d", len(a.Rows), len(b.Rows))
	for _, r := range a.Rows {
		if mb[r] < ma[r] {
			return msg + "; first missing: " + r
		}
	}
	for _, r := range b.Rows {
		if ma[r] < mb[r] {
			return msg + "; first added: " + r
		}
	}
	return msg
}

func deref(b *bool) any {
	if b == nil {
		return "no boolean"
	}
	return *b
}

// presentDiffers says where the reproducer's present differs from the run's pins (ADR-020 A3): said
// beside a match, never counted against it.
func presentDiffers(cfg *config.Config, m *QueryManifest) []string {
	var notes []string
	for _, k := range untrustedPins(cfg, m) {
		notes = append(notes, fmt.Sprintf("the run rests on the key %s, which is not trusted here", k))
	}
	if pv, err := currentView(cfg); err == nil {
		if b, err := json.Marshal(pv); err == nil && core.HashBytes(b) != m.View {
			notes = append(notes, "the current vocabulary (templates, aliases, row templates or signers' names) differs from the pinned one, which the reproduction used")
		}
	}
	var pk []string
	if usable, _, err := packages.Usable(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages); err == nil {
		for _, in := range usable {
			pk = append(pk, in.Package.ID+"@"+in.Revision.ID)
		}
	}
	sort.Strings(pk)
	if !bytes.Equal([]byte(strings.Join(pk, "\n")), []byte(strings.Join(m.Packages, "\n"))) {
		notes = append(notes, "the admitted packages differ from the run's")
	}
	return notes
}

func mustHex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

// untrustedPins are the keyids of the keys the run's trust pinned that none of this repository's tiers
// holds.
func untrustedPins(cfg *config.Config, m *QueryManifest) []string {
	now := map[string]bool{}
	for path := range cfg.TierPubkeys() {
		if b, err := os.ReadFile(path); err == nil {
			if pub, err := core.ParsePublicKeyHex(strings.TrimSpace(string(b))); err == nil {
				now[hex.EncodeToString(pub)] = true
			}
		}
	}
	var out []string
	for _, k := range m.Trust {
		if !now[k.Key] {
			out = append(out, core.KeyIDHex(mustHex(k.Key))+" ("+k.Tier+" there)")
		}
	}
	sort.Strings(out)
	return out
}

// runEnvelope is the run's signed record, from whichever registry of the union holds it.
func runEnvelope(ctx context.Context, cfg *config.Config, id string) (core.Envelope, bool) {
	inputs, err := binaries.Inputs(ctx, cfg)
	if err != nil {
		return core.Envelope{}, false
	}
	for _, in := range inputs {
		if p, err := binaries.OpenPlanktonSource(in.PlanktonDir); err == nil {
			if env, ok := p.Envelope(id); ok {
				return env, true
			}
		}
	}
	return core.Envelope{}, false
}
