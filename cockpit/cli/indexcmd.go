package cli

// indexcmd.go is `cockpit index`: the machine's index daemon (ADR-012), an operator's business.
//
//	cockpit index status    is it running, from which image, and how many registries it holds
//	cockpit index start     start it now rather than at the first query
//	cockpit index stop      stop it; its store stays
//	cockpit index rebuild   empty it; every registry is read again at the next query that reads it

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
)

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
	asJSON := len(args) == 2 && args[0] == "status" && args[1] == "--json"
	if len(args) != 1 && !asJSON {
		return fmt.Errorf("usage: cockpit index status [--json]|start|stop|rebuild")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
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
	return fmt.Errorf("usage: cockpit index status|start|stop|rebuild")
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
