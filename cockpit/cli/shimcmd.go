package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit"
)

// runShim serves the dispatcher of the tool shims (ADR-014): `cockpit shim serve --socket PATH`
// for a unix socket a container mounts, or `--listen ADDR` for TCP on this machine.
func runShim(ctx context.Context, args []string) error {
	const use = "usage: cockpit shim serve --socket PATH | --listen ADDR"
	if len(args) != 3 || args[0] != "serve" {
		return fmt.Errorf(use)
	}
	var ln net.Listener
	var err error
	switch args[1] {
	case "--socket":
		if len(args[2]) > 100 {
			return fmt.Errorf("--socket %s: a unix socket path may have about 100 characters, this one has %d — choose a shorter one", args[2], len(args[2]))
		}
		os.Remove(args[2]) // a socket left by an earlier serve
		ln, err = net.Listen("unix", args[2])
	case "--listen":
		if host, _, _ := strings.Cut(args[2], ":"); host != "127.0.0.1" && host != "localhost" && host != "[::1]" {
			return fmt.Errorf("--listen %s: the dispatcher signs records; serve it on this machine only (127.0.0.1:PORT), or over --socket", args[2])
		}
		ln, err = net.Listen("tcp", args[2])
	default:
		return fmt.Errorf(use)
	}
	if err != nil {
		return err
	}
	c, err := cockpit.Open(ctx, cockpit.Start{})
	if err != nil {
		return err
	}
	defer c.Close()
	fmt.Fprintf(os.Stderr, "cockpit: tool shims answered at %s\n", ln.Addr())
	return http.Serve(ln, c.CallHandler())
}
