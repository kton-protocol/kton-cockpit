// Command cockpit is the kton-cockpit command line with the backends that ship with it (git, local).
// The command line itself is the package cockpit/cli, so a binary that adds an extension backend
// (ADR-008) is the same program with one more import.
package main

import "github.com/kton-protocol/kton-cockpit/cockpit/cli"

func main() { cli.Main() }
