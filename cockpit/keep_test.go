package cockpit

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kton.dev/plankton/blobstore"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A say keeps the bytes of the files it signs about, so the signed version opens after the working copy
// changed (B11, MH 23.11.): a tracked file in .kton/blobs, committed with the claim; an ignored one in
// .kton/local/blobs only; one over claims.keepMaxBytes, or with claims.keep off, not at all. The answer
// says which, with hash and size. The word form and a batch keep alike, and a file field is kept too.
func TestSay_KeepsTheSignedFiles(t *testing.T) {
	ctx := context.Background()
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes", "evidence")
	r.Write(t, filepath.Join(config.Dir, "templates", "evidence.json"),
		`{"name":"evidence","target":"either","predicate":"https://example.org/evidence","fields":{"report":{"type":"file","required":true}}}`)
	r.Write(t, ".gitignore", "secret/\n")
	r.WriteConfig(t, raw)
	r.Use(t)
	c := New(Start{})
	cfg := r.Config(t)
	shared, _ := blobstore.Open(cfg.BlobsDir)
	local, _ := blobstore.Open(filepath.Join(cfg.LocalDir, "blobs"))
	committed := func(h string) bool {
		x := strings.TrimPrefix(h, "sha256:")
		out, _ := exec.Command("git", "-C", r.Root, "ls-tree", "-r", "--name-only", "HEAD", "--", ".kton/blobs/"+x[:2]+"/"+x).Output()
		return strings.TrimSpace(string(out)) != ""
	}
	say := func(subject string, fields map[string]string, tmpl string) *SayResult {
		t.Helper()
		res, err := c.Say(ctx, SayRequest{Subject: subject, Template: tmpl, Fields: fields})
		if err != nil {
			t.Fatalf("say about %s: %v", subject, err)
		}
		return res
	}

	r.Write(t, "data/tracked.csv", "a,b\n1,2\n")
	h := core.HashBytes([]byte("a,b\n1,2\n"))
	res := say("data/tracked.csv", map[string]string{"value": "v"}, "describes")
	if len(res.Kept) != 1 || res.Kept[0].Where != "shared" || res.Kept[0].Hash != h || res.Kept[0].Size != 8 {
		t.Fatalf("a tracked subject: kept shared with its hash and size: %+v", res.Kept)
	}
	if !shared.Has(h) || !committed(h) {
		t.Errorf("its bytes are in .kton/blobs and committed with the claim (has %v)", shared.Has(h))
	}
	// The working copy changes: the signed version is still there by its hash.
	r.Write(t, "data/tracked.csv", "a,b\n1,3\n")
	if b, err := shared.Get(h); err != nil || string(b) != "a,b\n1,2\n" {
		t.Errorf("the signed version opens by its hash: %q %v", b, err)
	}

	r.Write(t, "secret/notes.txt", "kept out of history")
	hs := core.HashBytes([]byte("kept out of history"))
	res = say("secret/notes.txt", map[string]string{"value": "v"}, "describes")
	if len(res.Kept) != 1 || res.Kept[0].Where != "local" || shared.Has(hs) || !local.Has(hs) {
		t.Errorf("an ignored file: kept in this checkout only: %+v (shared %v, local %v)", res.Kept, shared.Has(hs), local.Has(hs))
	}

	r.Write(t, "data/report.txt", "evidence bytes")
	he := core.HashBytes([]byte("evidence bytes"))
	res = say("urn:example:x", map[string]string{"report": "data/report.txt"}, "evidence")
	if len(res.Kept) != 1 || res.Kept[0].Path != "data/report.txt" || res.Kept[0].Where != "shared" || !shared.Has(he) {
		t.Errorf("a file field: kept shared: %+v", res.Kept)
	}

	// The word form passes the file to the core, which keeps it.
	r.Write(t, "data/word.csv", "word form")
	hw := core.HashBytes([]byte("word form"))
	if out, code := r.Cockpit(t, "say", "describes", "data/word.csv", "value=v"); code != 0 {
		t.Fatalf("say in the word form: %d %s", code, out)
	}
	if !shared.Has(hw) {
		t.Error("the word form keeps the file it signs about")
	}

	// A batch keeps each claim's file.
	r.Write(t, "data/batch.csv", "batch")
	hb := core.HashBytes([]byte("batch"))
	batch, err := c.SayBatch(ctx, SayBatchRequest{Claims: []SayRequest{{Subject: "data/batch.csv", Template: "describes", Fields: map[string]string{"value": "v"}}}})
	if err != nil || len(batch.Claims[0].Kept) != 1 || batch.Claims[0].Kept[0].Where != "shared" || !shared.Has(hb) {
		t.Errorf("a batch keeps its claims' files: %+v %v", batch, err)
	}

	// Over the limit, and with keep off: not kept, and said.
	raw.Claims.KeepMaxBytes = 4
	r.WriteConfig(t, raw)
	r.Write(t, "data/big.csv", "more than four bytes")
	res = say("data/big.csv", map[string]string{"value": "v"}, "describes")
	if len(res.Kept) != 1 || res.Kept[0].Where != "no" || !strings.Contains(res.Kept[0].Why, "keepMaxBytes") || shared.Has(core.HashBytes([]byte("more than four bytes"))) {
		t.Errorf("over the limit: not kept: %+v", res.Kept)
	}
	raw.Claims.KeepMaxBytes, raw.Claims.Keep = 0, "off"
	r.WriteConfig(t, raw)
	r.Write(t, "data/off.csv", "keep is off")
	res = say("data/off.csv", map[string]string{"value": "v"}, "describes")
	if len(res.Kept) != 1 || res.Kept[0].Where != "no" || shared.Has(core.HashBytes([]byte("keep is off"))) {
		t.Errorf("claims.keep off: not kept: %+v", res.Kept)
	}
}
