package cli

import (
	"strings"
	"testing"
)

// `cockpit <command> --help` prints that command's usage and runs nothing: `init --help` used to
// scaffold the current directory and `mcp --help` to start the server.
func TestCommandHelp_AnswersBeforeTheCommandRuns(t *testing.T) {
	for _, c := range []string{"mcp", "init", "show", "doctor", "scope", "run", "keygen", "pin", "trust",
		"federation", "sync", "config", "index", "version"} {
		for _, flag := range []string{"--help", "-h"} {
			help, ok := helpFor([]string{c, flag})
			if !ok {
				t.Fatalf("%s %s: not answered as help, the command would run", c, flag)
			}
			if !strings.Contains(help, "cockpit "+c) || strings.Contains(help, "cockpit - kton-cockpit") {
				t.Errorf("%s %s: want the command's own lines, got:\n%s", c, flag, help)
			}
		}
	}
	// The verbs answer --help with their fields, and a command with other arguments runs.
	for _, args := range [][]string{{"ask", "--help"}, {"publish", "-h"}, {"init"}, {"doctor", "--json"}} {
		if _, ok := helpFor(args); ok {
			t.Errorf("%v: taken as command help", args)
		}
	}
}
