// Package anchor witnesses a signed record in a Sigstore Rekor transparency log and stores the
// proof beside it.
//
// The kernel assigns this here in as many words: `kton anchor`'s own source says it "is not part of
// the kernel — plankton records fotons and never needs the network — it is a cockpit-invoked
// capability", and that the verified Rekor coordinates "are meant to be stored as a sidecar on the
// claim/foton". So this package invokes it and attaches what comes back; it implements no
// transparency-log logic of its own.
//
// What an anchor adds, precisely: a signature says WHO signed, not WHEN, and a signer who later
// produces a different record can claim that one was the original. Rekor attests that this exact
// record existed by a given time, in an append-only log a third party can audit. `kton anchor` does
// not merely submit — it verifies the inclusion proof and the Signed Entry Timestamp, and then
// verifies that the entry BINDS to this envelope and this verifier, so a hostile endpoint replaying
// a real but unrelated entry is rejected rather than reported as anchored.
package anchor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/show"
)

// Kind selects which substrate holds the record, which decides both the pubkey to anchor under and
// the binary that stores the resulting evidence.
type Kind int

const (
	Foton Kind = iota
	Claim
)

// Entry is what Rekor answered, as far as this cockpit reports it. The full entry is stored beside
// the record; these are the coordinates worth handing back to a caller.
type Entry struct {
	LogIndex int64  `json:"logIndex"`
	UUID     string `json:"uuid"`
}

// Record anchors one record and attaches the verified entry to it as verification material.
//
// The evidence is stored under the `rekor-entry` scheme of SPEC §8.1 — external evidence ABOUT a
// record, which the kernel keeps as opaque bytes and never evaluates, exactly as it stores a DSSE
// signature without checking it on ingest. Verification is this cockpit's business, and it already
// happened: `kton anchor` refuses rather than prints on any of the three checks failing.
func Record(ctx context.Context, cfg *config.Config, recordID string, kind Kind) (*Entry, error) {
	env, err := show.EnvelopeFor(ctx, cfg, recordID)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "cockpit-anchor-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	envPath := filepath.Join(dir, "record.dsse.json")
	if err := os.WriteFile(envPath, env, 0o600); err != nil {
		return nil, err
	}

	pubkey := cfg.PlanktonKey + ".pub"
	binary := "plankton"
	if kind == Claim {
		pubkey = cfg.NektonKey + ".pub"
		binary = "nekton"
	}
	// The signing key's own public half: the anchor is submitted under the identity that signed the
	// record, so Rekor's entry binds to that verifier and not to some other key this repo holds.
	pubkey = trimKeySuffix(pubkey)

	out, err := run(ctx, cfg, filepath.Join(cfg.BinDir, "kton"), "anchor", envPath, pubkey)
	if err != nil {
		return nil, fmt.Errorf("anchoring %s in Rekor failed: %w", recordID, err)
	}
	entryJSON, entry, err := parseAnchorOutput(out)
	if err != nil {
		return nil, err
	}

	entryPath := filepath.Join(dir, "rekor-entry.json")
	if err := os.WriteFile(entryPath, entryJSON, 0o600); err != nil {
		return nil, err
	}
	if _, err := run(ctx, cfg, filepath.Join(cfg.BinDir, binary),
		"attach", recordID, "--scheme", "rekor-entry", "--file", entryPath); err != nil {
		return nil, fmt.Errorf("the record was anchored but attaching the proof failed: %w", err)
	}
	return entry, nil
}

// parseAnchorOutput reads the entry out of what `kton anchor` prints. It emits two human lines and
// then the entry JSON on the same stream, so the JSON is found by its opening brace rather than by
// position — a third human line would otherwise silently shift the parse. Raised upstream as a
// request for `--json`, the same gap #39 and #57 closed elsewhere.
func parseAnchorOutput(out string) (json.RawMessage, *Entry, error) {
	i := bytes.IndexByte([]byte(out), '{')
	if i < 0 {
		return nil, nil, fmt.Errorf("kton anchor printed no entry:\n%s", out)
	}
	raw := json.RawMessage(out[i:])
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, nil, fmt.Errorf("could not read the Rekor entry kton anchor printed: %w", err)
	}
	if e.UUID == "" {
		return nil, nil, fmt.Errorf("kton anchor returned an entry with no uuid:\n%s", out)
	}
	return raw, &e, nil
}

// run invokes a kernel binary with this repo's registries pinned, plus the Rekor settings the
// config carries. They are passed as environment because that is the interface `kton anchor`
// offers — and they come from the verified config, never inherited from the ambient shell.
func run(ctx context.Context, cfg *config.Config, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = cfg.RepoRoot
	cmd.Env = []string{
		"PLANKTON_DIR=" + cfg.PlanktonDir,
		"NEKTON_DIR=" + cfg.NektonDir,
		"NEKTON_TEMPLATES=" + cfg.TemplatesDir,
		"HOME=" + os.Getenv("HOME"), // some TLS trust stores are looked up relative to it
		"SSL_CERT_FILE=" + os.Getenv("SSL_CERT_FILE"),
		"SSL_CERT_DIR=" + os.Getenv("SSL_CERT_DIR"),
	}
	if cfg.Raw.Anchor.RekorURL != "" {
		cmd.Env = append(cmd.Env, "KTON_REKOR_URL="+cfg.Raw.Anchor.RekorURL)
	}
	if cfg.Raw.Anchor.RekorPubkey != "" {
		cmd.Env = append(cmd.Env, "KTON_REKOR_PUBKEY="+cfg.Raw.Anchor.RekorPubkey)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s %v: %w\n%s", filepath.Base(bin), args, err, stderr.String())
	}
	return stdout.String(), nil
}

// trimKeySuffix turns keys/x.key.pub into keys/x.pub — the config names the private half, and the
// public half sits beside it under the same stem.
func trimKeySuffix(p string) string {
	return filepath.Join(filepath.Dir(p),
		trimSuffix(filepath.Base(p), ".key.pub")+".pub")
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
