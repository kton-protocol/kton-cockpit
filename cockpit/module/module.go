// Package module is the door through which a module joins the cockpit (ADR-011 section 5, specified in
// ADR-015 section 1). A module is a Go package that registers itself from init:
//
//	func init() {
//		module.Register(module.Module{Name: "tool", Commands: ..., Profiles: ..., Words: ...})
//	}
//
// and is linked by importing it. A module reaches the core only through Publish, Say, Ask, the executor
// and the index's read side. It never opens a registry and never signs anything itself.
//
// What a module brings:
//   - Commands, the operator commands of `cockpit <name> …`. They are CLI only (SPEC §6) and never on
//     MCP: what an agent may do goes through the three verbs.
//   - Profiles, the package profiles `cockpit install` hands to this module: a package of such a
//     profile is installed with `cockpit <name> install`, not with plain install.
//   - Words, the templates and named queries it brings (templates/<name>.json, queries/<name>.rq),
//     compiled into the binary and so the same in every repository. They are admitted like a
//     package's: only when the configuration names the module in claims.allowedModules.
//
// This package imports nothing of the cockpit, so the core can consult it without a cycle.
package module

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

// Command is one operator command of a module: `cockpit <module> <Name> args…`.
type Command struct {
	Name  string
	Usage string // one line: the arguments and what it does
	Run   func(ctx context.Context, args []string) error
}

// Module is what a module registers.
type Module struct {
	Name     string    // the operator command group: `cockpit <name> …`
	Commands []Command // operator commands, CLI only (SPEC §6): never on MCP
	Profiles []string  // the package profiles `cockpit install` hands to this module
	Words    fs.FS     // templates/<name>.json and queries/<name>.rq; nil when it brings none
	// Guard decides whether a request at the door may be answered (ADR-019 section 4); nil: the module
	// guards nothing. Decide asks every module's guard.
	Guard func(ctx context.Context, r Request) Decision
}

// Reserved are the core's own commands. A module cannot take one of them as its name.
var Reserved = []string{
	"mcp", "init", "show", "doctor", "scope", "publish", "say", "ask", "version", "run", "keygen", "pin",
	"trust", "federation", "sync", "config", "index", "install", "workflow", "shim", "ui", "help",
}

var (
	mu      sync.RWMutex
	modules = map[string]Module{}
)

// Register adds a module. It is called from init, so a mistake is a programming error and panics:
// a name taken twice or reserved by the core, a command named twice or without Run, a profile claimed
// by two modules, or Words that do not read.
func Register(m Module) {
	if err := register(m); err != nil {
		panic("cockpit module: " + err.Error())
	}
}

func register(m Module) error {
	mu.Lock()
	defer mu.Unlock()
	if m.Name == "" || strings.ContainsAny(m.Name, " /:") || strings.HasPrefix(m.Name, "-") {
		return fmt.Errorf("a module needs a plain name, not %q", m.Name)
	}
	for _, r := range Reserved {
		if m.Name == r {
			return fmt.Errorf("%q is a core command, not a module name", m.Name)
		}
	}
	if _, dup := modules[m.Name]; dup {
		return fmt.Errorf("the module %q is registered twice", m.Name)
	}
	seen := map[string]bool{}
	for _, c := range m.Commands {
		if c.Name == "" || c.Run == nil {
			return fmt.Errorf("module %q: a command needs a name and Run", m.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("module %q: the command %q is declared twice", m.Name, c.Name)
		}
		seen[c.Name] = true
	}
	for _, p := range m.Profiles {
		for _, other := range modules {
			for _, q := range other.Profiles {
				if p == q {
					return fmt.Errorf("the profile %s is claimed by both %q and %q", p, other.Name, m.Name)
				}
			}
		}
	}
	if _, _, err := words(m); err != nil {
		return fmt.Errorf("module %q: %w", m.Name, err)
	}
	modules[m.Name] = m
	return nil
}

// Lookup returns the module registered under name.
func Lookup(name string) (Module, bool) {
	mu.RLock()
	defer mu.RUnlock()
	m, ok := modules[name]
	return m, ok
}

// All returns the registered modules, sorted by name.
func All() []Module {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Module, 0, len(modules))
	for _, m := range modules {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ForProfile returns the module that a package of this profile is handed to.
func ForProfile(profile string) (Module, bool) {
	mu.RLock()
	defer mu.RUnlock()
	for _, m := range modules {
		for _, p := range m.Profiles {
			if p == profile {
				return m, true
			}
		}
	}
	return Module{}, false
}

// Command returns the module's command of this name.
func (m Module) Command(name string) (Command, bool) {
	for _, c := range m.Commands {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// Templates returns the templates the module brings, by name.
func (m Module) Templates() (map[string][]byte, error) {
	t, _, err := words(m)
	return t, err
}

// Queries returns the named queries the module brings, by name.
func (m Module) Queries() (map[string][]byte, error) {
	_, q, err := words(m)
	return q, err
}

// words reads templates/*.json and queries/*.rq from the module's Words. Anything else there is an
// error rather than ignored, as a stray file in a repository's template directory is.
func words(m Module) (templates, queries map[string][]byte, err error) {
	templates, queries = map[string][]byte{}, map[string][]byte{}
	if m.Words == nil {
		return templates, queries, nil
	}
	err = fs.WalkDir(m.Words, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == "." || p == "templates" || p == "queries" {
				return nil
			}
			return fmt.Errorf("words: unexpected directory %s (only templates/ and queries/)", p)
		}
		dir, file := path.Split(p)
		var dst map[string][]byte
		var ext string
		switch dir {
		case "templates/":
			dst, ext = templates, ".json"
		case "queries/":
			dst, ext = queries, ".rq"
		default:
			return fmt.Errorf("words: %s is neither in templates/ nor in queries/", p)
		}
		if !strings.HasSuffix(file, ext) {
			return fmt.Errorf("words: %s should end in %s", p, ext)
		}
		b, err := fs.ReadFile(m.Words, p)
		if err != nil {
			return err
		}
		dst[strings.TrimSuffix(file, ext)] = b
		return nil
	})
	return templates, queries, err
}
