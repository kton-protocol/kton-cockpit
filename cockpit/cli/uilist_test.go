package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// ui list --json lists every installed package for a frontend: name, revision, profile, whether
// the configuration admits it, and what fails its check, under refused rather than hidden
// (userinterface's PR 2, C2.4).
func TestUIList_ListsInstalledPackagesWithWhatFailsTheirCheck(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	c := cockpit.New(cockpit.Start{})
	build := func(name string) string {
		src := t.TempDir()
		os.MkdirAll(filepath.Join(src, "templates"), 0o755)
		os.WriteFile(filepath.Join(src, "templates", name+".json"), []byte(`{"name": "`+name+`", "target": "either",
  "predicate": "https://example.org/`+name+`", "fields": {"note": {"type": "string"}}}`), 0o644)
		_, key, _ := ed25519.GenerateKey(rand.Reader)
		dir := filepath.Join(t.TempDir(), name)
		if _, _, err := scope.BuildTemplates(dir, name, "v1", filepath.Join(src, "templates"), "", key, scope.Options{}); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	if _, err := c.Install(ctx, cockpit.InstallRequest{Path: build("admitted"), Allow: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Install(ctx, cockpit.InstallRequest{Path: build("plain")}); err != nil {
		t.Fatal(err)
	}
	// A package directory whose store is not a package: listed, refused, not hidden.
	r.Write(t, ".kton/packages/broken@v1/nekton/garbage.jsonl", "not a store\n")

	stdout, stderr, code := r.CockpitSplit(t, "ui", "list", "--json")
	var l UIListing
	if code != 0 || json.Unmarshal([]byte(stdout), &l) != nil {
		t.Fatalf("ui list --json (exit %d):\n%s\n%s", code, stdout, stderr)
	}
	by := map[string]UIPackage{}
	for _, p := range l.Packages {
		by[p.Dir] = p
	}
	if p := by[".kton/packages/admitted@v1"]; p.Name != "admitted" || !p.Allowed || p.Revision == "" || len(p.Refused) != 0 {
		t.Errorf("the admitted package: %+v", p)
	}
	if p := by[".kton/packages/plain@v1"]; p.Name != "plain" || p.Allowed || len(p.Refused) != 0 || p.UI != nil {
		t.Errorf("the installed, not admitted package (declarations null): %+v", p)
	}
	if p := by[".kton/packages/broken@v1"]; len(p.Refused) != 1 || p.Refused[0].Code != "package.check" || p.Refused[0].Reason == "" || p.UI != nil {
		t.Errorf("the broken package is not listed with its refusal, ui null: %+v", p)
	}
	// An admitted package that declares nothing for a frontend says so with empty lists, not null.
	if p := by[".kton/packages/admitted@v1"]; p.UI == nil || len(p.UI) != 0 || p.Blocks == nil || p.Compositions == nil {
		t.Errorf("an admitted templates package's declarations should be empty lists: %+v", p)
	}
}

// A verticle's ui/, blocks and compositions are listed as ktonpkg (v0.9) reads them, each under its
// file name, for a frontend to read instead of the package folder (C2.4). The package is ktonpkg's own
// test verticle (scope/testdata/hostile/good), read from the module ktonpkg is built from.
func TestUIList_ListsAVerticlesUIBlocksAndCompositions(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/gitmick/ktonpkg").Output()
	if err != nil {
		t.Fatalf("locating ktonpkg: %v", err)
	}
	good := filepath.Join(strings.TrimSpace(string(out)), "scope", "testdata", "hostile", "good")
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	dir := filepath.Join(t.TempDir(), "review")
	if _, _, err := scope.BuildVerticle(dir, "review", "v1", good, key, scope.Options{}); err != nil {
		t.Fatalf("building ktonpkg's good verticle: %v", err)
	}
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: dir, Allow: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	stdout, stderr, code := r.CockpitSplit(t, "ui", "list", "--json")
	var l struct {
		Packages []struct {
			Name         string           `json:"name"`
			Allowed      bool             `json:"allowed"`
			UI           []map[string]any `json:"ui"`
			Blocks       []map[string]any `json:"blocks"`
			Compositions []map[string]any `json:"compositions"`
			Refused      []UIRefusal      `json:"refused"`
		} `json:"packages"`
	}
	if code != 0 || json.Unmarshal([]byte(stdout), &l) != nil || len(l.Packages) != 1 {
		t.Fatalf("ui list --json (exit %d):\n%s\n%s", code, stdout, stderr)
	}
	p := l.Packages[0]
	if p.Name != "review" || !p.Allowed || len(p.Refused) != 0 {
		t.Fatalf("the verticle: %+v", p)
	}
	if len(p.UI) != 1 || p.UI[0]["file"] != "ui/review.json" || p.UI[0]["slot"] == nil {
		t.Errorf("ui entries: %v", p.UI)
	}
	if len(p.Blocks) != 1 || p.Blocks[0]["name"] != "table" || p.Blocks[0]["viewer"] == nil {
		t.Errorf("blocks: %v", p.Blocks)
	}
	if len(p.Compositions) != 1 || p.Compositions[0]["name"] != "list" || p.Compositions[0]["kind"] == nil {
		t.Errorf("compositions: %v", p.Compositions)
	}
}

// ui list gives each usable package its members (role, name, hash), each composition its hash, and
// each ui entry its path; ui cat serves a member's bytes by hash, and only a member's, so a frontend
// never reads a package folder (userinterface's PR 2; ktonpkg v0.9.6 Revision.Members).
func TestUICat_ServesAMembersBytesAndNothingElse(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/gitmick/ktonpkg").Output()
	if err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(strings.TrimSpace(string(out)), "scope", "testdata", "hostile", "good")
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	dir := filepath.Join(t.TempDir(), "review")
	if _, _, err := scope.BuildVerticle(dir, "review", "v1", good, key, scope.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: dir, Allow: true}); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := r.CockpitSplit(t, "ui", "list", "--json")
	var l struct {
		Packages []struct {
			UI           []map[string]any `json:"ui"`
			Compositions []map[string]any `json:"compositions"`
			Members      []UIMember       `json:"members"`
		} `json:"packages"`
	}
	if code != 0 || json.Unmarshal([]byte(stdout), &l) != nil || len(l.Packages) != 1 {
		t.Fatalf("ui list --json (exit %d):\n%s\n%s", code, stdout, stderr)
	}
	p := l.Packages[0]
	if len(p.Members) == 0 {
		t.Fatalf("no members listed: %s", stdout)
	}
	if p.UI[0]["file"] != "ui/review.json" {
		t.Errorf("a ui entry's file should be its path, got %v", p.UI[0]["file"])
	}
	hash, _ := p.Compositions[0]["hash"].(string)
	if !strings.HasPrefix(hash, "sha256:") {
		t.Fatalf("the composition carries no hash: %v", p.Compositions[0])
	}

	body, stderr2, code := r.CockpitSplit(t, "ui", "cat", "review", hash)
	want, _ := os.ReadFile(filepath.Join(good, "ui", "compositions", "list.json"))
	if code != 0 || body != string(want) {
		t.Fatalf("ui cat of the composition (exit %d): got %q, want the file's bytes\n%s", code, body, stderr2)
	}
	for _, args := range [][]string{
		{"ui", "cat", "review", "sha256:" + strings.Repeat("0", 64)},
		{"ui", "cat", "nosuch", hash},
	} {
		if out, code := r.Cockpit(t, args...); code != 2 {
			t.Errorf("%v: want a refusal (exit 2), got exit %d:\n%s", args, code, out)
		}
	}
}

// ui cat looks only among usable packages: an unadmitted package of the same name does not stand in
// the way, and the package can be named by dir or id too (kton-review U3 on #170); a package that is
// installed but not admitted, or whose bytes changed after install, is refused ui.package and has
// members null in ui list (T7).
func TestUICat_OnlyUsablePackagesAndByNameDirOrID(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	ctx := context.Background()
	c := cockpit.New(cockpit.Start{})
	build := func(name, label string, allow bool, templates map[string]string) string {
		src := t.TempDir()
		os.MkdirAll(filepath.Join(src, "templates"), 0o755)
		for f, b := range templates {
			os.WriteFile(filepath.Join(src, "templates", f+".json"), []byte(b), 0o644)
		}
		_, key, _ := ed25519.GenerateKey(rand.Reader)
		dir := filepath.Join(t.TempDir(), name+label)
		if _, _, err := scope.BuildTemplates(dir, name, label, filepath.Join(src, "templates"), "", key, scope.Options{}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Install(ctx, cockpit.InstallRequest{Path: dir, Allow: allow}); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	build("words", "v1", true, map[string]string{"notes": tmpl("notes", "https://example.org/notes")})
	build("words", "v2", false, map[string]string{"other": tmpl("other", "https://example.org/other")}) // same name, not admitted
	build("plain", "v1", false, map[string]string{"memo": tmpl("memo", "https://example.org/memo")})
	build("changed", "v1", true, map[string]string{"x": tmpl("x", "https://example.org/x")})

	stdout, _, _ := r.CockpitSplit(t, "ui", "list", "--json")
	var l struct {
		Packages []struct {
			Name    string     `json:"name"`
			Package string     `json:"package"`
			Dir     string     `json:"dir"`
			Allowed bool       `json:"allowed"`
			Members []UIMember `json:"members"`
		} `json:"packages"`
	}
	if json.Unmarshal([]byte(stdout), &l) != nil {
		t.Fatalf("ui list --json: %s", stdout)
	}
	var words struct{ id, dir, hash string }
	plainHash := ""
	for _, p := range l.Packages {
		switch {
		case p.Name == "words" && p.Allowed:
			words.id, words.dir, words.hash = p.Package, p.Dir, p.Members[0].Hash
		case p.Name == "plain":
			if p.Members != nil {
				t.Errorf("an unadmitted package lists members: %+v", p.Members)
			}
		}
	}
	// The plain package's member hash, read from its own folder, to ask for it by hash.
	pp, err := scope.Open(filepath.Join(r.Root, ".kton", "packages", "plain@v1"))
	if err != nil {
		t.Fatal(err)
	}
	prev, _, _ := pp.Sealed()
	if ms, err := prev.Members(); err == nil && len(ms) > 0 {
		plainHash = ms[0].Hash
	}

	for _, ref := range []string{"words", words.dir, words.id} {
		if _, stderr, code := r.CockpitSplit(t, "ui", "cat", ref, words.hash); code != 0 {
			t.Errorf("ui cat %s: exit %d\n%s", ref, code, stderr)
		}
	}
	if out, code := r.Cockpit(t, "ui", "cat", "plain", plainHash); code != 2 || !strings.Contains(out, "not a usable installed package") {
		t.Errorf("ui cat of an unadmitted package (exit %d):\n%s", code, out)
	}

	blobs, _ := filepath.Glob(filepath.Join(r.Root, ".kton", "packages", "changed@v1", "blobs", "*", "*"))
	cp, _ := scope.Open(filepath.Join(r.Root, ".kton", "packages", "changed@v1"))
	crev, _, _ := cp.Sealed()
	cms, _ := crev.Members()
	for _, b := range blobs {
		os.WriteFile(b, []byte("tampered"), 0o644)
	}
	if out, code := r.Cockpit(t, "ui", "cat", "changed", cms[0].Hash); code != 2 || !strings.Contains(out, "not a usable installed package") {
		t.Errorf("ui cat of a package changed after install (exit %d):\n%s", code, out)
	}
	stdout, _, _ = r.CockpitSplit(t, "ui", "list", "--json")
	json.Unmarshal([]byte(stdout), &l)
	for _, p := range l.Packages {
		if p.Name == "changed" && p.Members != nil {
			t.Errorf("a package changed after install lists members")
		}
	}
}

// A reference that names two usable packages, one's name being the other's id, is refused rather than
// resolved by order (kton-review T8 on #170).
func TestUICat_AReferenceNamingTwoPackagesIsRefused(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	buildPkg(t, filepath.Join(t.TempDir(), "first"), "first", "v1", key, map[string]string{"notes": tmpl("notes", "https://example.org/notes")})
	stdout, _, _ := r.CockpitSplit(t, "ui", "list", "--json")
	var l struct {
		Packages []struct {
			Name    string     `json:"name"`
			Package string     `json:"package"`
			Members []UIMember `json:"members"`
		} `json:"packages"`
	}
	if json.Unmarshal([]byte(stdout), &l) != nil || len(l.Packages) != 1 || len(l.Packages[0].Members) == 0 {
		t.Fatalf("ui list --json: %s", stdout)
	}
	id, hash := l.Packages[0].Package, l.Packages[0].Members[0].Hash
	buildPkg(t, filepath.Join(t.TempDir(), "second"), id, "v1", key, map[string]string{"memo": tmpl("memo", "https://example.org/memo")})

	out, code := r.Cockpit(t, "ui", "cat", id, hash)
	if code != 2 || !strings.Contains(out, "names both") {
		t.Errorf("ui cat by a reference two packages answer to (exit %d):\n%s", code, out)
	}
	if _, stderr, code := r.CockpitSplit(t, "ui", "cat", "first", hash); code != 0 {
		t.Errorf("ui cat by the unambiguous name: exit %d\n%s", code, stderr)
	}
}

// ui cat --json reads several members in one call, in the order asked, each the bytes the single form
// gives; a hash that is not a member refuses the whole call with nothing on stdout (SPEC §6).
func TestUICat_SeveralMembersInOneCall(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	buildPkg(t, filepath.Join(t.TempDir(), "words"), "words", "v1", key, map[string]string{
		"notes": tmpl("notes", "https://example.org/notes"), "memo": tmpl("memo", "https://example.org/memo")})
	stdout, _, _ := r.CockpitSplit(t, "ui", "list", "--json")
	var l struct {
		Packages []struct {
			Members []UIMember `json:"members"`
		} `json:"packages"`
	}
	if json.Unmarshal([]byte(stdout), &l) != nil || len(l.Packages) != 1 || len(l.Packages[0].Members) < 2 {
		t.Fatalf("ui list --json: %s", stdout)
	}
	ms := l.Packages[0].Members
	a, b := ms[1].Hash, ms[0].Hash

	stdout, stderr, code := r.CockpitSplit(t, "ui", "cat", "words", a, b, "--json")
	var got UICat
	if code != 0 || json.Unmarshal([]byte(stdout), &got) != nil || len(got.Members) != 2 {
		t.Fatalf("ui cat words a b --json (exit %d): %s\n%s", code, stdout, stderr)
	}
	for i, h := range []string{a, b} {
		single, _, _ := r.CockpitSplit(t, "ui", "cat", "words", h)
		if got.Members[i].Hash != h || string(got.Members[i].Bytes) != single {
			t.Errorf("member %d is %s with %d bytes, want %s as the single form gives it", i, got.Members[i].Hash, len(got.Members[i].Bytes), h)
		}
	}
	if got.Name != "words" || got.Package == "" {
		t.Errorf("the answer does not name the package: %+v", got)
	}

	missing := "sha256:" + strings.Repeat("0", 64)
	stdout, stderr, code = r.CockpitSplit(t, "ui", "cat", "words", a, missing, "--json")
	var ref struct {
		Code    string          `json:"code"`
		Members json.RawMessage `json:"members"`
	}
	if code != 2 || json.Unmarshal([]byte(stdout), &ref) != nil || ref.Code != "ui.member" || ref.Members != nil {
		t.Errorf("a non-member among several is not refused alone (exit %d), stdout %q:\n%s", code, stdout, stderr)
	}
	if _, _, code := r.CockpitSplit(t, "ui", "cat", "words", a, b); code == 0 {
		t.Error("several hashes without --json are not refused: their bytes would run together")
	}
}

// A package whose member claim, signed with its own key, names a path that climbs out of its folder is
// refused: install refuses it, and one an older cockpit installed and admitted is listed with no members
// and package.check, and ui cat serves nothing from it (kton-review F1 on #174). The fixture was built
// with ktonpkg v0.9.8, which still signed such a name; v0.9.9 refuses it when writing.
func TestUIList_APackageWithAClimbingMemberNameIsRefused(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	fixture, err := filepath.Abs(filepath.Join("testdata", "hostile-words"))
	if err != nil {
		t.Fatal(err)
	}
	const h = "sha256:41b20806979a13f9037e99c61a755ce56f9dc5f3e1933605dc68b68170cb0a64" // the climbing member
	if _, err := cockpit.New(cockpit.Start{}).Install(context.Background(), cockpit.InstallRequest{Path: fixture, Allow: true}); err == nil {
		t.Error("install admits a package with a climbing member name")
	}
	if err := os.CopyFS(filepath.Join(r.Root, ".kton", "packages", "words@v1"), os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	p, err := scope.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	raw := r.Config(t).Raw
	raw.Claims.AllowedPackages = append(raw.Claims.AllowedPackages, p.ID)
	r.WriteConfig(t, raw)

	stdout, _, _ := r.CockpitSplit(t, "ui", "list", "--json")
	var l struct {
		Packages []struct {
			Name    string      `json:"name"`
			Members []UIMember  `json:"members"`
			Refused []UIRefusal `json:"refused"`
		} `json:"packages"`
	}
	if json.Unmarshal([]byte(stdout), &l) != nil {
		t.Fatalf("ui list --json: %s", stdout)
	}
	found := false
	for _, pk := range l.Packages {
		if pk.Name != "words" {
			continue
		}
		found = true
		if pk.Members != nil || len(pk.Refused) == 0 || pk.Refused[0].Code != "package.check" {
			t.Errorf("a package with a climbing member name is listed as usable: members %v, refused %+v", pk.Members, pk.Refused)
		}
	}
	if !found {
		t.Fatalf("the package is not listed: %s", stdout)
	}
	if out, code := r.Cockpit(t, "ui", "cat", "words", h); code != 2 || !strings.Contains(out, "not a usable installed package") {
		t.Errorf("ui cat serves the climbing member (exit %d):\n%s", code, out)
	}
}

// ui list lists members only for what ui cat serves: two admitted packages of one name are both
// unusable, so both are listed with members null and package.unusable (the PR #174 review).
func TestUIList_MembersOnlyForUsablePackages(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	for _, d := range []string{"a", "b"} {
		_, key, _ := ed25519.GenerateKey(rand.Reader)
		buildPkg(t, filepath.Join(t.TempDir(), d), "words", "v1-"+d, key, map[string]string{"notes": tmpl("notes", "https://example.org/notes")})
	}
	stdout, _, _ := r.CockpitSplit(t, "ui", "list", "--json")
	var l struct {
		Packages []struct {
			Allowed bool        `json:"allowed"`
			Members []UIMember  `json:"members"`
			Refused []UIRefusal `json:"refused"`
		} `json:"packages"`
	}
	if json.Unmarshal([]byte(stdout), &l) != nil || len(l.Packages) != 2 {
		t.Fatalf("ui list --json: %s", stdout)
	}
	for _, p := range l.Packages {
		if !p.Allowed || p.Members != nil || len(p.Refused) != 1 || p.Refused[0].Code != "package.unusable" {
			t.Errorf("an admitted package ui cat refuses is listed with members %v, refused %+v", p.Members, p.Refused)
		}
	}
}

// ui list gives each usable package where it was built from (source), the packages it declares it needs
// (requires) and every place its compositions name another package (references), as ktonpkg v0.10.0
// reads them (architecture view G7). Like the declarations, they are null for a package a frontend may
// not use.
func TestUIList_NamesSourceRequiresAndReferences(t *testing.T) {
	r := testrepo.New(t)
	r.Use(t)
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/gitmick/ktonpkg").Output()
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	if err := os.CopyFS(src, os.DirFS(filepath.Join(strings.TrimSpace(string(out)), "scope", "testdata", "hostile", "good"))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, scope.RequiresFile), []byte(`{"packages": [{"package": "words", "why": "its templates"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	source := &scope.Source{Repo: "https://example.org/review.git", Commit: strings.Repeat("a", 40), Path: "verticles/review"}
	c := cockpit.New(cockpit.Start{})
	for _, v := range []struct {
		name  string
		allow bool
	}{{"review", true}, {"other", false}} {
		dir := filepath.Join(t.TempDir(), v.name)
		if _, _, err := scope.BuildVerticle(dir, v.name, "v1", src, key, scope.Options{Source: source}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Install(context.Background(), cockpit.InstallRequest{Path: dir, Allow: v.allow}); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, code := r.CockpitSplit(t, "ui", "list", "--json")
	var l struct {
		Packages []struct {
			Name       string            `json:"name"`
			Source     *scope.Source     `json:"source"`
			Requires   []map[string]any  `json:"requires"`
			References []json.RawMessage `json:"references"`
			Refused    []UIRefusal       `json:"refused"`
		} `json:"packages"`
	}
	if code != 0 || json.Unmarshal([]byte(stdout), &l) != nil || len(l.Packages) != 2 {
		t.Fatalf("ui list --json (exit %d):\n%s\n%s", code, stdout, stderr)
	}
	for _, p := range l.Packages {
		switch p.Name {
		case "review":
			if len(p.Refused) != 0 {
				t.Fatalf("review refused: %+v", p.Refused)
			}
			if p.Source == nil || *p.Source != *source {
				t.Errorf("source: %+v, want %+v", p.Source, source)
			}
			if len(p.Requires) != 1 || p.Requires[0]["package"] != "words" || p.Requires[0]["why"] != "its templates" {
				t.Errorf("requires: %v", p.Requires)
			}
			if p.References == nil {
				t.Errorf("references: null for a usable package, want a list")
			}
		case "other":
			if p.Source != nil || p.Requires != nil || p.References != nil {
				t.Errorf("a package not admitted lists source, requires or references: %+v", p)
			}
		}
	}
}
