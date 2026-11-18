package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit/module"
)

// runModule runs `cockpit <module> <command> args…`: a module's operator command (ADR-015 section 1).
// Operator commands are CLI only; nothing here is on MCP.
func runModule(ctx context.Context, m module.Module, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(moduleHelp(m))
		if len(args) == 0 {
			return fmt.Errorf("usage: cockpit %s <command> …", m.Name)
		}
		return nil
	}
	c, ok := m.Command(args[0])
	if !ok {
		fmt.Print(moduleHelp(m))
		return fmt.Errorf("the %s module has no command %q", m.Name, args[0])
	}
	err := c.Run(ctx, args[1:])
	exitIfRefused(err) // a module's refusal exits as the core's do
	return err
}

func moduleHelp(m module.Module) string {
	var b strings.Builder
	fmt.Fprintf(&b, "cockpit %s — operator commands of the %s module (CLI only)\n\n", m.Name, m.Name)
	for _, c := range m.Commands {
		fmt.Fprintf(&b, "  cockpit %s %-10s %s\n", m.Name, c.Name, c.Usage)
	}
	return b.String()
}

// moduleUsage is the usage's lines for the linked modules, one per module.
func moduleUsage() string {
	var b strings.Builder
	for _, m := range module.All() {
		names := make([]string, 0, len(m.Commands))
		for _, c := range m.Commands {
			names = append(names, c.Name)
		}
		fmt.Fprintf(&b, "  cockpit %-8s %s — module (cockpit %s help)\n", m.Name, strings.Join(names, " / "), m.Name)
	}
	return b.String()
}
