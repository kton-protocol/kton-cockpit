// uilist-fixture writes what `cockpit ui list --json` will answer once kton-cockpit fills ui, blocks and
// compositions from ktonpkg (v0.5.3, shape agreed 19.11.), for the installed packages of a checkout. It is a
// stand-in for the e2e only, read by the shell through the setting kton.ui.listFixture; with cockpit v0.5.3
// the setting goes and the shell calls the verb.
//
//	uilist-fixture <repo root>  > ui-list.json
//
// Per package: name, package (id), revision, label, dir, allowed (claims.allowedPackages), ui[], blocks[],
// compositions[] (with the member hash), refused[] (the package's own check, and VerticleProblems), and
// members[] {role, name, hash}: every member's content hash, which the shell asks of kton-cockpit so it can
// load what it shows by hash.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg/scope"
)

type refusal struct {
	File   string `json:"file"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

type member struct {
	Role string `json:"role"`
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type pkg struct {
	Name         string           `json:"name,omitempty"`
	Package      string           `json:"package,omitempty"`
	Revision     string           `json:"revision,omitempty"`
	Label        string           `json:"label,omitempty"`
	Profile      string           `json:"profile,omitempty"`
	Dir          string           `json:"dir"`
	Allowed      bool             `json:"allowed"`
	UI           []map[string]any `json:"ui"`
	Blocks       []map[string]any `json:"blocks"`
	Compositions []map[string]any `json:"compositions"`
	Refused      []refusal        `json:"refused"`
	Members      []member         `json:"members"`
}

func asMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: uilist-fixture <repo root>")
		os.Exit(2)
	}
	root := os.Args[1]
	var cfg struct {
		Claims struct {
			AllowedPackages []string `json:"allowedPackages"`
		} `json:"claims"`
	}
	if b, err := os.ReadFile(filepath.Join(root, ".kton", "cockpit.config.json")); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	allowed := map[string]bool{}
	for _, id := range cfg.Claims.AllowedPackages {
		allowed[id] = true
	}
	dirs, _ := os.ReadDir(filepath.Join(root, ".kton", "packages"))
	out := struct {
		Packages []pkg `json:"packages"`
	}{Packages: []pkg{}}
	for _, d := range dirs {
		if !d.IsDir() || !strings.Contains(d.Name(), "@") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(".kton", "packages", d.Name()))
		p := pkg{Dir: rel, UI: []map[string]any{}, Blocks: []map[string]any{}, Compositions: []map[string]any{}, Refused: []refusal{}, Members: []member{}}
		fail := func(err error) {
			p.Refused = append(p.Refused, refusal{Code: "package.check", Reason: err.Error()})
			out.Packages = append(out.Packages, p)
		}
		sp, err := scope.Open(filepath.Join(root, rel))
		if err != nil {
			fail(err)
			continue
		}
		r, _, err := sp.Sealed()
		if err != nil {
			fail(err)
			continue
		}
		p.Name, p.Package, p.Revision, p.Label = sp.Name, sp.ID, r.ID, r.Label
		p.Profile, _ = r.Profile()
		p.Allowed = allowed[sp.ID]
		for _, why := range sp.Verify() {
			p.Refused = append(p.Refused, refusal{Code: "package.check", Reason: why})
		}
		for _, why := range r.VerticleProblems() {
			p.Refused = append(p.Refused, refusal{Code: "ui.check", Reason: why})
		}
		claims, err := r.Claims()
		if err != nil {
			fail(err)
			continue
		}
		hashes := map[string]string{} // role/name -> hash
		for _, c := range claims {
			if c.Predicate != scope.PartOf {
				continue
			}
			v, _ := c.Object["value"].(map[string]any)
			role, _ := v["role"].(string)
			name, _ := v["name"].(string)
			if role == "" || name == "" {
				continue
			}
			p.Members = append(p.Members, member{Role: role, Name: name, Hash: c.Subject})
			hashes[role+"/"+name] = c.Subject
		}
		sort.Slice(p.Members, func(i, j int) bool {
			return p.Members[i].Role+p.Members[i].Name < p.Members[j].Role+p.Members[j].Name
		})
		entries, _ := r.UIEntries()
		viewers, _ := r.Viewers()
		blocks, _ := r.Blocks()
		comps, _ := r.Compositions()
		for _, n := range sortedKeys(entries) {
			e := asMap(entries[n])
			e["file"] = "ui/" + n + ".json"
			p.UI = append(p.UI, e)
		}
		for _, n := range sortedKeys(blocks) {
			b := asMap(blocks[n])
			b["name"] = n
			delete(b, "block")
			if s, ok := b["viewer"].(string); ok { // a viewers/<name>.json declaration: the pin itself
				if v, ok := viewers[s]; ok {
					b["viewer"] = map[string]any{"viewer": v.Viewer, "at": v.At, "tree": v.Tree}
				}
			}
			p.Blocks = append(p.Blocks, b)
		}
		for _, n := range sortedKeys(comps) {
			c := asMap(comps[n])
			delete(c, "parts")
			delete(c, "eachPart")
			delete(c, "composition")
			c["name"] = n
			c["hash"] = hashes["composition/"+n]
			p.Compositions = append(p.Compositions, c)
		}
		out.Packages = append(out.Packages, p)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
