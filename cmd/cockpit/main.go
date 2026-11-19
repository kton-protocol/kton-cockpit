// Command cockpit is the kton-cockpit command line with the backends that ship with it (git, local)
// and the modules it links (tool, ADR-015). The command line itself is the package cockpit/cli, so a
// binary that adds an extension backend (ADR-008) or a module is the same program with one more import.
package main

import (
	"github.com/kton-protocol/kton-cockpit/cockpit/cli"
	_ "github.com/kton-protocol/kton-cockpit/cockpit/tool"
)

func main() { cli.Main() }
