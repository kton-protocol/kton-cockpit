package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit/module"
)

var moduleRan []string

func init() {
	module.Register(module.Module{Name: "clitest", Commands: []module.Command{
		{Name: "hello", Usage: "<who>   greet", Run: func(_ context.Context, args []string) error {
			moduleRan = append(moduleRan, strings.Join(args, " "))
			return nil
		}},
	}})
}

// A linked module's operator commands are `cockpit <module> <command>`, listed in the usage; an
// unknown command of a module is an error that shows the module's commands (ADR-015 section 1).
func TestModule_CommandsAreReachableAndListed(t *testing.T) {
	m, _ := module.Lookup("clitest")
	if err := runModule(context.Background(), m, []string{"hello", "world"}); err != nil {
		t.Fatal(err)
	}
	if len(moduleRan) != 1 || moduleRan[0] != "world" {
		t.Errorf("the command got %v", moduleRan)
	}
	if err := runModule(context.Background(), m, []string{"nope"}); err == nil || !strings.Contains(err.Error(), `no command "nope"`) {
		t.Errorf("unknown command: %v", err)
	}
	if u := usageText(); !strings.Contains(u, "cockpit clitest  hello — module") {
		t.Errorf("the usage does not list the module:\n%s", u)
	}
	if h, ok := helpFor([]string{"clitest", "--help"}); !ok || !strings.Contains(h, "clitest") {
		t.Errorf("clitest --help = %q", h)
	}
}
