// Command cockpit is the kton-cockpit command line with the backends that ship with it (git, local)
// and the modules it links (tool, ADR-015). The command line itself is the package cockpit/cli, so a
// binary that adds an extension backend (ADR-008) or a module is the same program with one more import.
package main

import (
	"github.com/kton-protocol/kton-cockpit/cockpit/cli"
	_ "github.com/kton-protocol/kton-cockpit/cockpit/tool"
	"kton.dev/plankton/keys"
)

func main() {
	// First, before anything starts a child (ADR-021 §2.3): cli.Main takes it too, for a composed binary.
	keys.TakeEnvPassphrase()
	cli.Main()
}
