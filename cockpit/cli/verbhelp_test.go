package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/cockpit"
)

// `cockpit <verb> --help` names the fields a call takes, nested ones included, from the request
// types the MCP schemas come from. It used to answer "not valid JSON".
func TestVerbHelp_NamesTheFields(t *testing.T) {
	cases := []struct {
		verb string
		help func(*bytes.Buffer) error
		want []string
	}{
		{"publish", func(b *bytes.Buffer) error { return verbHelp[cockpit.PublishRequest](b, "publish") },
			[]string{"cmd", "inputs", "outputs", "envRef"}},
		{"say", func(b *bytes.Buffer) error { return verbHelp[cockpit.SayRequest](b, "say") },
			[]string{"template", "subject"}},
		{"ask", func(b *bytes.Buffer) error { return verbHelp[cockpit.AskRequest](b, "ask") },
			[]string{"query", "ref", "filter", "trustTier", "minReproductions"}},
	}
	for _, c := range cases {
		var b bytes.Buffer
		if err := c.help(&b); err != nil {
			t.Fatalf("%s: %v", c.verb, err)
		}
		lines := map[string]bool{}
		for _, l := range strings.Split(b.String(), "\n") {
			// A field line is a name and a type; a description line is prose under it.
			if f := strings.Fields(l); len(f) >= 2 && isSchemaType(strings.TrimSuffix(f[1], "[]")) {
				lines[f[0]] = true
			}
		}
		for _, w := range c.want {
			if !lines[w] {
				t.Errorf("%s --help does not list %s:\n%s", c.verb, w, b.String())
			}
		}
		// The negative control: a name that is no field is not listed.
		if lines["output"] {
			t.Errorf("%s --help lists a field that does not exist", c.verb)
		}
	}
}

func isSchemaType(s string) bool {
	switch s {
	case "string", "integer", "number", "boolean", "object", "any":
		return true
	}
	return false
}
