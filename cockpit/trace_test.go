package cockpit

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// COCKPIT_TRACE prints a say's phases with their durations, so a slow say is measured where it is slow
// (userinterface, T12); without it, nothing is printed.
func TestSay_ATraceNamesItsPhases(t *testing.T) {
	r := testrepo.New(t)
	raw := testrepo.DefaultConfig()
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	r.WriteConfig(t, raw)
	r.Use(t)
	var buf bytes.Buffer
	traceOut = &buf
	t.Cleanup(func() { traceOut = os.Stderr })
	say := func() {
		t.Helper()
		if _, err := New(Start{}).Say(context.Background(), SayRequest{Subject: "urn:example:x", Template: "describes", Fields: map[string]string{"value": "v"}}); err != nil {
			t.Fatal(err)
		}
	}
	say()
	if buf.Len() != 0 {
		t.Errorf("without COCKPIT_TRACE nothing is printed: %q", buf.String())
	}
	t.Setenv("COCKPIT_TRACE", "1")
	say()
	for _, want := range []string{"prepare", "scope's head", "sign and store", "persist", "confirm", "total"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("the trace names %q:\n%s", want, buf.String())
		}
	}
}
