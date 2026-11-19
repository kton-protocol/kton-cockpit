package cockpit

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kton-protocol/kton-cockpit/cockpit/module"
)

func init() {
	module.Register(module.Module{Name: "wordstest", Words: fstest.MapFS{
		"templates/noted-by-module.json": {Data: []byte(`{
  "name": "noted-by-module",
  "target": "either",
  "predicate": "https://kton.dev/v/noted-by-module",
  "fields": {"note": {"type": "string", "required": true}}
}`)},
	}})
}

// A module's words are compiled in, and admitted like a package's: only when the configuration names
// the module in claims.allowedModules (ADR-015 section 1). A module named there that this binary does
// not link is an error, not words quietly missing.
func TestSay_ModuleWordsAreAdmittedByName(t *testing.T) {
	r := describesRepo(t)
	pub := publishOne(t, r)
	ctx := context.Background()
	say := func() (bool, string) {
		res, _, err := Say(ctx, nil, SayRequest{Subject: pub.FotonID, Template: "noted-by-module",
			Fields: map[string]string{"note": "from a module"}})
		if err != nil {
			return false, err.Error()
		}
		return !res.IsError, errText(res)
	}
	if ok, msg := say(); ok {
		t.Fatalf("a module's template was used without claims.allowedModules: %s", msg)
	}

	raw := r.Config(t).Raw
	raw.Claims.AllowedModules = []string{"wordstest"}
	r.WriteConfig(t, raw)
	if ok, msg := say(); !ok {
		t.Fatalf("say with the module admitted: %s", msg)
	}

	raw.Claims.AllowedModules = []string{"wordstest", "not-linked"}
	r.WriteConfig(t, raw)
	if ok, msg := say(); ok || !strings.Contains(msg, "does not link") {
		t.Fatalf("a module the binary does not link must be refused by name, got ok=%v %s", ok, msg)
	}
}
