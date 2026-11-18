package tool_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/cockpit/tool"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// With the tool module linked, `cockpit install` hands a tool package to it: plain install refuses
// and names `cockpit tool install`, which installs it (ADR-015 section 1, step T1). The module does
// not take packages of other profiles.
func TestInstall_HandsToolPackagesToTheModule(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	c := cockpit.New(cockpit.Start{})
	dir := sealed(t, "nonmem", scope.ProfileTool)

	_, err := c.Install(ctx, cockpit.InstallRequest{Path: dir})
	var ref *cockpit.Refusal
	if !errors.As(err, &ref) || ref.Code != "package.module" || !strings.Contains(ref.Reason, "cockpit tool install") {
		t.Fatalf("plain install of a tool package: want refusal package.module naming cockpit tool install, got %v", err)
	}
	out, err := tool.Install(ctx, c, dir)
	if err != nil {
		t.Fatalf("cockpit tool install: %v", err)
	}
	if out.Profile != scope.ProfileTool || out.Dir != "packages/nonmem@v1" {
		t.Errorf("installed %+v", out)
	}

	other := sealed(t, "words", scope.ProfileTemplates)
	if _, err := tool.Install(ctx, c, other); !errors.As(err, &ref) || ref.Code != "package.module" {
		t.Errorf("the tool module took a templates package: %v", err)
	}
	if _, err := c.Install(ctx, cockpit.InstallRequest{Path: other}); err != nil {
		t.Errorf("plain install of a templates package: %v", err)
	}
}

func sealed(t *testing.T, name, profile string) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	dir := filepath.Join(t.TempDir(), name)
	p, err := scope.Create(dir, name, key, scope.Options{})
	must(t, err)
	rev, err := p.NewRevision("v1")
	must(t, err)
	must(t, rev.SetProfile(profile))
	_, err = rev.Seal()
	must(t, err)
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
