// Package tool is the cockpit's first module (ADR-015): tool packages, one environment class per
// program. Importing it links the module; the public binary does (cmd/cockpit), and a build without it
// behaves as before, with plain `cockpit install` taking tool packages.
//
// This is the skeleton (ADR-015 step T1): the module registers, `cockpit install` hands it the
// tool profile, and `cockpit tool install` installs a tool package as install does. The derived
// binding, replacement and uninstall (T2), hidden configuration (T3) and qualification (T4) follow.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gitmick/ktonpkg/scope"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/cockpit/module"
)

// Name is the module's name: `cockpit tool …`.
const Name = "tool"

func init() {
	module.Register(module.Module{
		Name: Name,
		Commands: []module.Command{
			{Name: "install", Usage: "<package-dir> [--json]   install a tool package (profile/tool)", Run: runInstall},
		},
		Profiles: []string{scope.ProfileTool},
		OneShot:  true, // install runs and ends
	})
}

func runInstall(ctx context.Context, args []string) error {
	var dir string
	asJSON := false
	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case dir == "" && len(a) > 0 && a[0] != '-':
			dir = a
		default:
			return fmt.Errorf("usage: cockpit tool install <package-dir> [--json]")
		}
	}
	if dir == "" {
		return fmt.Errorf("usage: cockpit tool install <package-dir> [--json]")
	}
	out, err := Install(ctx, cockpit.New(cockpit.Start{}), dir)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	fmt.Print(cockpit.FormatInstall(out))
	return nil
}

// Install installs a tool package through the core's install, as the module that claims the tool
// profile.
func Install(ctx context.Context, c *cockpit.Cockpit, dir string) (*cockpit.InstallResult, error) {
	return c.Install(ctx, cockpit.InstallRequest{Path: dir, Via: Name})
}
