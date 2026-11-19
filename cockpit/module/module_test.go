package module

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func noop(context.Context, []string) error { return nil }

// registerT registers m for the length of one test.
func registerT(t *testing.T, m Module) error {
	t.Helper()
	err := register(m)
	if err == nil {
		t.Cleanup(func() { mu.Lock(); delete(modules, m.Name); mu.Unlock() })
	}
	return err
}

// The door refuses what would make two things answer to one name: a core command, a module twice, a
// command twice, a profile claimed by two modules, and words that are neither templates nor queries
// (ADR-015 section 1).
func TestRegister_RefusesWhatWouldBeAmbiguous(t *testing.T) {
	if err := registerT(t, Module{Name: "alpha", Profiles: []string{"https://kton.dev/profile/x"},
		Commands: []Command{{Name: "go", Run: noop}}}); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string]Module{
		"core command":    {Name: "install"},
		"taken twice":     {Name: "alpha"},
		"empty name":      {Name: ""},
		"command twice":   {Name: "beta", Commands: []Command{{Name: "a", Run: noop}, {Name: "a", Run: noop}}},
		"command no run":  {Name: "beta", Commands: []Command{{Name: "a"}}},
		"profile claimed": {Name: "beta", Profiles: []string{"https://kton.dev/profile/x"}},
		"stray word":      {Name: "beta", Words: fstest.MapFS{"notes.txt": {Data: []byte("x")}}},
		"wrong suffix":    {Name: "beta", Words: fstest.MapFS{"queries/q.sparql": {Data: []byte("x")}}},
	} {
		if err := registerT(t, m); err == nil {
			t.Errorf("%s: registered, want a refusal", name)
		}
	}
	if m, ok := ForProfile("https://kton.dev/profile/x"); !ok || m.Name != "alpha" {
		t.Errorf("ForProfile = %q %v, want alpha", m.Name, ok)
	}
	if _, ok := ForProfile("https://kton.dev/profile/y"); ok {
		t.Error("an unclaimed profile has a module")
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "core command") {
			t.Errorf("Register of a core command name should panic, got %v", r)
		}
	}()
	Register(Module{Name: "doctor"})
}

// Words are read by kind and named by file: templates/<name>.json, queries/<name>.rq.
func TestWords_ByKindAndName(t *testing.T) {
	m := Module{Name: "gamma", Words: fstest.MapFS{
		"templates/schedules.json": {Data: []byte(`{"name":"schedules"}`)},
		"queries/jobs-in-force.rq": {Data: []byte("SELECT * WHERE { ?s ?p ?o }")},
	}}
	if err := registerT(t, m); err != nil {
		t.Fatal(err)
	}
	got, _ := Lookup("gamma")
	ts, err := got.Templates()
	if err != nil || string(ts["schedules"]) != `{"name":"schedules"}` || len(ts) != 1 {
		t.Errorf("templates = %v %v", ts, err)
	}
	qs, err := got.Queries()
	if err != nil || len(qs) != 1 || qs["jobs-in-force"] == nil {
		t.Errorf("queries = %v %v", qs, err)
	}
}
