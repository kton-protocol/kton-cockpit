package cockpit

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"

	"context"
	"github.com/gitmick/ktonpkg/scope"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
	"os/exec"
	"sync"
	"testing"
)

// Claims said at the same time into one scope chain one after another: each takes the head the one
// before it wrote, so the scope keeps one head (qm-dev-provenance, 20.11.: five cockpit_say calls to
// one MCP process without waiting gave five claims on the same prev).
func TestScope_ConcurrentSaysChainOneAfterAnother(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	const n = 5
	var wg sync.WaitGroup
	errs := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, _, err := Say(context.Background(), nil, SayRequest{Subject: pub.FotonID, Template: "working-on",
				Scope: "review", Fields: map[string]string{"step": "analysis", "by-session": string(rune('a' + i))}})
			switch {
			case err != nil:
				errs[i] = err.Error()
			case res.IsError:
				errs[i] = errText(res)
			}
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != "" {
			t.Errorf("say %d: %s", i, e)
		}
	}
	if _, length, branched := r.ScopeHead(t, scope); branched || length != n {
		t.Errorf("concurrent says: want one chain of %d, got length %d, branched %v", n, length, branched)
	}
}

// The same across processes that do not share a temp directory (sandboxed sessions, PrivateTmp,
// containers): the lock is the checkout's own, not the temp directory's (kton-review X1 on #183).
func TestScope_ConcurrentSayProcessesWithTheirOwnTempDirChainOneAfterAnother(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	r.Cockpit(t, "--version") // builds the binary once, before the processes race to
	const n = 5
	var wg sync.WaitGroup
	outs := make([]string, n)
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		cmd := r.CockpitCommand(t, "say", "working-on", pub.FotonID, "step=analysis", "by-session=p"+string(rune('a'+i)), "--scope", "review")
		cmd.Env = append(cmd.Env, "TMPDIR="+t.TempDir())
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := cmd.CombinedOutput()
			outs[i] = string(out)
			if ee, ok := err.(*exec.ExitError); ok {
				codes[i] = ee.ExitCode()
			} else if err != nil {
				codes[i] = -1
			}
		}(i)
	}
	wg.Wait()
	for i := range outs {
		if codes[i] != 0 {
			t.Errorf("say process %d (exit %d):\n%s", i, codes[i], outs[i])
		}
	}
	if _, length, branched := r.ScopeHead(t, scope); branched || length != n {
		t.Errorf("concurrent say processes: want one chain of %d, got length %d, branched %v", n, length, branched)
	}
}

// Two installs of one package at the same time seed its scope once: the install holds the claims lock
// from the package's records through the seed (kton-review M2, T15 on #183). A second seed of the name
// would make every later install refuse the scope as ambiguous.
func TestInstall_ConcurrentInstallsSeedTheScopeOnce(t *testing.T) {
	ctx := context.Background()
	r := testrepo.New(t)
	r.Use(t)
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "templates"), 0o755)
	os.WriteFile(filepath.Join(src, "templates", "requests.json"), []byte(`{"name": "requests", "target": "either",
  "predicate": "https://example.org/ln/requests", "fields": {"lane": {"type": "string"}}}`), 0o644)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	pkg := filepath.Join(t.TempDir(), "inbox")
	if _, _, err := scope.BuildTemplates(pkg, "inbox", "v1", filepath.Join(src, "templates"), "", key, scope.Options{}); err != nil {
		t.Fatal(err)
	}
	c := New(Start{})
	const n = 4
	var wg sync.WaitGroup
	scopes := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			inst, err := c.Install(ctx, InstallRequest{Path: pkg, Allow: true})
			errs[i] = err
			if inst != nil {
				scopes[i] = inst.Scope
			}
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Errorf("install %d: %v", i, errs[i])
		}
		if scopes[i] != scopes[0] {
			t.Errorf("install %d seeded scope %s, install 0 %s", i, scopes[i], scopes[0])
		}
	}
	if _, err := c.Install(ctx, InstallRequest{Path: pkg, Allow: true}); err != nil {
		t.Errorf("an install after the concurrent ones: %v", err)
	}
}
