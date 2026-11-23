package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/kton-protocol/kton-cockpit/cockpit"
)

// runReproduce is `cockpit reproduce` (ADR-020, section 4): a query run evaluated again over its pinned
// dataset, built from the registries; a match signs the run's own record, so its ↻N counts it.
func runReproduce(ctx context.Context, args []string) error {
	asJSON := hasFlag(args, "--json")
	var in cockpit.ReproduceRequest
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--json":
		case "--timeout":
			if i+1 >= len(args) {
				return fmt.Errorf("--timeout needs a number of seconds")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return fmt.Errorf("--timeout: %v", err)
			}
			in.TimeoutSeconds = n
		default:
			if in.Run != "" {
				return fmt.Errorf("reproduce takes one query run, not %s and %s", in.Run, a)
			}
			in.Run = a
		}
	}
	if in.Run == "" {
		return fmt.Errorf("usage: cockpit reproduce <query run id> [--timeout SECONDS] [--json]")
	}
	out, err := cockpit.New(cockpit.Start{}).Reproduce(ctx, in)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(out)
	}
	fmt.Printf("reproduced %s: %d rows, result %s\n  signed as %s\n", out.Run, out.Rows, out.Result, out.FotonID)
	for _, n := range out.Notes {
		fmt.Printf("  note: %s\n", n)
	}
	return nil
}
