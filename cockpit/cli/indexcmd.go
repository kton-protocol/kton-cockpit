package cli

// indexcmd.go is `cockpit index`: the machine's index daemon (ADR-012), an operator's business.
//
//	cockpit index status    is it running, from which image, and how many registries it holds
//	cockpit index start     start it now rather than at the first query
//	cockpit index stop      stop it; its store stays
//	cockpit index rebuild   empty it; every registry is read again at the next query that reads it
//	cockpit index registries  which registries it holds, how current, and which repositories read them

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// IndexRegistries is `index registries --json`: every registry the machine's index holds, and for each
// repository whose queries read one, the packages it admits (architecture view G10). Read only: it
// starts no daemon, and lists nothing when none answers.
type IndexRegistries struct {
	Kind       string              `json:"kind"`
	Answering  bool                `json:"answering"`
	Registries []oxigraph.Registry `json:"registries"`
	Roots      []IndexRoot         `json:"roots"`
}

// IndexRoot is a repository whose queries read the index, with the packages a frontend there may use.
type IndexRoot struct {
	Root     string         `json:"root"`
	Packages []IndexPackage `json:"packages"`
	Problem  string         `json:"problem,omitempty"`
}

// IndexPackage is one usable package of a repository: its name, id and sealed revision.
type IndexPackage struct {
	Name     string `json:"name"`
	Package  string `json:"package"`
	Revision string `json:"revision"`
}

func indexRegistries(ctx context.Context, cfg *config.Config) (IndexRegistries, error) {
	out := IndexRegistries{Kind: cfg.Raw.Index.KindOrDefault(), Registries: []oxigraph.Registry{}, Roots: []IndexRoot{}}
	var d *oxigraph.Daemon
	if cfg.Raw.Index.URL != "" {
		var err error
		if d, err = oxigraph.Ensure(ctx, cfg); err != nil {
			return out, err
		}
	} else {
		var err error
		if d, _, err = oxigraph.Running(ctx); err != nil || d == nil {
			return out, err
		}
	}
	regs, err := d.RegistryList(ctx)
	if err != nil {
		return out, err
	}
	out.Answering, out.Registries = true, regs
	roots := map[string]bool{}
	for _, r := range regs {
		for _, by := range r.ReadBy {
			roots[by.Root] = true
		}
	}
	names := make([]string, 0, len(roots))
	for r := range roots {
		names = append(names, r)
	}
	sort.Strings(names)
	for _, root := range names {
		out.Roots = append(out.Roots, indexRoot(ctx, cfg, root))
	}
	return out, nil
}

// indexRoot reads the packages a repository admits from its own configuration: the cockpit side of the
// join, since the store holds no packages.
func indexRoot(ctx context.Context, cfg *config.Config, root string) IndexRoot {
	out := IndexRoot{Root: root, Packages: []IndexPackage{}}
	rc := cfg
	if root != cfg.RepoRoot {
		var err error
		if rc, err = config.Load(ctx, root); err != nil {
			out.Problem = err.Error()
			return out
		}
		if rc.RepoRoot != root {
			out.Problem = fmt.Sprintf("its configuration is read from %s (COCKPIT_REPO_DIR)", rc.RepoRoot)
			return out
		}
	}
	usable, _, err := packages.Usable(rc.RepoRoot, rc.Raw.Claims.AllowedPackages)
	if err != nil {
		out.Problem = err.Error()
		return out
	}
	for _, in := range usable {
		out.Packages = append(out.Packages, IndexPackage{Name: in.Package.Name, Package: in.Package.ID, Revision: in.Revision.ID})
	}
	sort.Slice(out.Packages, func(i, j int) bool { return out.Packages[i].Name < out.Packages[j].Name })
	return out
}

// IndexStatus is `index status --json`: the configured index, and its daemon when one runs.
type IndexStatus struct {
	Kind     string       `json:"kind"`
	URL      string       `json:"url,omitempty"` // index.url: a daemon this cockpit does not start
	Daemon   *IndexDaemon `json:"daemon,omitempty"`
	Store    string       `json:"store,omitempty"`
	Warnings []string     `json:"warnings,omitempty"`
	// Building are this repository's views being built now, with since and phase (userinterface, T12):
	// a frontend waiting on an ask shows them rather than guessing from timeouts.
	Building []oxigraph.Building `json:"building,omitempty"`
}

// IndexDaemon is the daemon this machine runs. Answering is false when it was started but does
// not answer; the next query starts it again.
type IndexDaemon struct {
	URL        string `json:"url"`
	Container  string `json:"container"`
	Answering  bool   `json:"answering"`
	Image      string `json:"image,omitempty"`
	Schema     int    `json:"schema,omitempty"`
	Memory     string `json:"memory,omitempty"`
	Registries int    `json:"registries,omitempty"`
}

func indexStatus(ctx context.Context, cfg *config.Config) (IndexStatus, error) {
	st := IndexStatus{Kind: cfg.Raw.Index.KindOrDefault(), URL: cfg.Raw.Index.URL}
	if st.URL != "" {
		return st, nil
	}
	d, l, err := oxigraph.Running(ctx)
	if err != nil {
		return st, err
	}
	if l != nil {
		st.Daemon = &IndexDaemon{URL: l.URL, Container: l.Container}
	}
	if d != nil {
		n, err := d.Registries(ctx)
		if err != nil {
			return st, err
		}
		st.Daemon.Answering = true
		st.Daemon.Image, st.Daemon.Schema, st.Daemon.Memory, st.Daemon.Registries = l.Image, l.Schema, l.MemoryOrDefault(), n
		if want := cfg.Raw.Index.MemoryOrDefault(); config.MemoryAbove(want, l.MemoryOrDefault()) {
			st.Warnings = append(st.Warnings, fmt.Sprintf("this configuration asks for %s (index.memory): the next query restarts it with that, on the same store", want))
		}
		if l.Schema != oxigraph.Schema {
			st.Warnings = append(st.Warnings, fmt.Sprintf("its store schema is not this cockpit's (%d): cockpit index stop", oxigraph.Schema))
		}
	}
	st.Store, _ = oxigraph.Dir()
	if st.Store != "" {
		st.Building = oxigraph.BuildsRunning(st.Store, cfg.RepoRoot)
	}
	return st, nil
}

func runIndex(ctx context.Context, args []string) error {
	asJSON := len(args) == 2 && (args[0] == "status" || args[0] == "registries") && args[1] == "--json"
	if len(args) != 1 && !asJSON {
		return fmt.Errorf("usage: cockpit index status [--json]|registries [--json]|start|stop|rebuild")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	if args[0] == "registries" {
		l, err := indexRegistries(ctx, cfg)
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(l)
		}
		printIndexRegistries(l)
		return nil
	}
	if asJSON {
		st, err := indexStatus(ctx, cfg)
		if err != nil {
			return err
		}
		return printJSON(st)
	}
	fmt.Printf("index.kind: %s\n", cfg.Raw.Index.KindOrDefault())
	switch args[0] {
	case "status":
		st, err := indexStatus(ctx, cfg)
		if err != nil {
			return err
		}
		if st.URL != "" {
			fmt.Printf("connects to %s (index.url); this cockpit starts nothing\n", st.URL)
			return nil
		}
		switch d := st.Daemon; {
		case d == nil:
			fmt.Println("daemon: not started")
		case !d.Answering:
			fmt.Printf("daemon: not answering at %s (container %s) — the next query starts it again\n", d.URL, d.Container)
		default:
			fmt.Printf("daemon: %s, container %s\n  image  %s\n  schema %d\n  memory %s\n  registries %d\n", d.URL, d.Container, d.Image, d.Schema, d.Memory, d.Registries)
			for _, w := range st.Warnings {
				fmt.Printf("  ! %s\n", w)
			}
		}
		fmt.Printf("store: %s\n", st.Store)
		for _, b := range st.Building {
			fmt.Printf("building: view %s since %s (%s), %s\n", b.Slot, b.Since.Local().Format("15:04:05"), time.Since(b.Since).Round(time.Second), b.Phase)
		}
		return nil
	case "start":
		d, err := oxigraph.Ensure(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("daemon: %s\n", d.URL)
		return nil
	case "stop":
		if cfg.Raw.Index.URL != "" {
			return fmt.Errorf("index.url names a daemon this cockpit did not start; stop it where it runs")
		}
		if err := oxigraph.Stop(ctx, cfg); err != nil {
			return err
		}
		fmt.Println("daemon: stopped (its store stays)")
		return nil
	case "rebuild":
		d, err := oxigraph.Ensure(ctx, cfg)
		if err != nil {
			return err
		}
		if err := d.Clear(ctx); err != nil {
			return err
		}
		fmt.Printf("emptied %s; each registry is read again at the next query that reads it\n", redact(d.URL))
		return nil
	}
	return fmt.Errorf("usage: cockpit index status|registries|start|stop|rebuild")
}

func printIndexRegistries(l IndexRegistries) {
	if !l.Answering {
		fmt.Println("no index daemon answers: nothing to list (cockpit index status)")
		return
	}
	if len(l.Registries) == 0 {
		fmt.Println("the index holds no registry yet")
	}
	for _, r := range l.Registries {
		where := r.Dir
		if where == "" {
			where = r.ID + " (directory not noted yet)"
		}
		state := ""
		if r.Stale != nil && *r.Stale {
			state = "  stale: read again at the next query"
		}
		fmt.Printf("%s\n  %d record(s), %d key(s), cursor %d%s\n", where, r.Records, len(r.Keys), r.Cursor, state)
		for _, by := range r.ReadBy {
			fmt.Printf("  read for %s, last %s\n", by.Root, by.LastRead)
		}
	}
	for _, root := range l.Roots {
		if root.Problem != "" {
			fmt.Printf("\n%s: %s\n", root.Root, root.Problem)
			continue
		}
		fmt.Printf("\n%s admits %d package(s)\n", root.Root, len(root.Packages))
		for _, p := range root.Packages {
			fmt.Printf("  %s  %s\n", p.Name, shortID(p.Revision))
		}
	}
}

// redact drops credentials an index.url might carry.
func redact(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	p.User = nil
	return p.String()
}
