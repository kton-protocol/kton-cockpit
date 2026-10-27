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

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
)

func runIndex(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: cockpit index status|start|stop|rebuild")
	}
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	fmt.Printf("index.kind: %s\n", cfg.Raw.Index.KindOrDefault())
	switch args[0] {
	case "status":
		if cfg.Raw.Index.URL != "" {
			fmt.Printf("connects to %s (index.url); this cockpit starts nothing\n", cfg.Raw.Index.URL)
			return nil
		}
		d, l, err := oxigraph.Running(ctx)
		if err != nil {
			return err
		}
		switch {
		case l == nil:
			fmt.Println("daemon: not started")
		case d == nil:
			fmt.Printf("daemon: not answering at %s (container %s) — the next query starts it again\n", l.URL, l.Container)
		default:
			n, err := d.Registries(ctx)
			if err != nil {
				return err
			}
			fmt.Printf("daemon: %s, container %s\n  image  %s\n  schema %d\n  memory %s\n  registries %d\n", l.URL, l.Container, l.Image, l.Schema, l.MemoryOrDefault(), n)
			if want := cfg.Raw.Index.MemoryOrDefault(); config.MemoryAbove(want, l.MemoryOrDefault()) {
				fmt.Printf("  ! this configuration asks for %s (index.memory): the next query restarts it with that, on the same store\n", want)
			}
			if l.Schema != oxigraph.Schema {
				fmt.Printf("  ! its store schema is not this cockpit's (%d): cockpit index stop\n", oxigraph.Schema)
			}
		}
		dir, _ := oxigraph.Dir()
		fmt.Printf("store: %s\n", dir)
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
