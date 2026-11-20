package cli

import (
	"context"
	"fmt"

	"github.com/kton-protocol/kton-cockpit/cockpit"
)

// runLoad is `cockpit load` (ADR-017, SPEC §12.6): a file's RDF into the index, as a graph named by its
// hash, for this repository's queries. An operator command: it signs nothing.
func runLoad(ctx context.Context, args []string) error {
	asJSON := hasFlag(args, "--json")
	var files []string
	var as, drop string
	list := false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--json":
		case "--list":
			list = true
		case "--as", "--drop":
			if i+1 >= len(args) {
				return fmt.Errorf("%s needs a value", a)
			}
			i++
			if a == "--as" {
				as = args[i]
			} else {
				drop = args[i]
			}
		default:
			files = append(files, a)
		}
	}
	c := cockpit.New(cockpit.Start{})
	switch {
	case list:
		l, err := c.LoadedGraphs(ctx)
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(l)
		}
		if len(l.Loaded) == 0 && len(l.Missing) == 0 {
			fmt.Println("nothing is loaded for this repository")
		}
		for _, g := range l.Loaded {
			fmt.Printf("%s  %d triples  %s  from %s  (%s)\n", g.Hash, g.Triples, g.MediaType, g.From, g.Loaded)
		}
		for _, m := range l.Missing {
			fmt.Printf("%s  NOT LOADED: on this checkout's load list, from %s, but %s\n", m.Hash, m.From, m.Why)
		}
		return nil
	case drop != "":
		if err := c.DropLoaded(ctx, drop); err != nil {
			return err
		}
		if asJSON {
			return printJSON(map[string]string{"dropped": drop})
		}
		fmt.Printf("dropped %s\n", drop)
		return nil
	case len(files) == 0:
		return fmt.Errorf("usage: cockpit load <file|sha256:hex>... [--as TYPE] [--json] | cockpit load --list [--json] | cockpit load --drop <sha256:hex>")
	}
	out, err := c.Load(ctx, cockpit.LoadRequest{Files: files, As: as})
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(out)
	}
	for _, g := range out.Loaded {
		fmt.Printf("loaded %s  %d triples  as %s  from %s\n  query it as GRAPH <%s> { ... }\n", g.Hash, g.Triples, g.MediaType, g.From, g.Graph)
	}
	return nil
}
