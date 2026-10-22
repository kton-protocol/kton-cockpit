package cockpit

import (
	"context"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

const (
	describesIRI = "http://purl.org/dc/terms/description"
	rationaleIRI = "https://w3id.org/ep-plan#hasRationale"
)

// describesRepo admits describes and rationale the way `cockpit init` does.
func describesRepo(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	raw := r.Config(t).Raw
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes", "rationale")
	r.WriteConfig(t, raw)
	r.Use(t)
	return r
}

func sayValue(t *testing.T, subject, template, value string) string {
	t.Helper()
	res, out, err := Say(context.Background(), nil, SayRequest{Subject: subject, Template: template,
		Fields: map[string]string{"value": value}})
	if err != nil || res.IsError {
		t.Fatalf("say %s: err=%v result=%+v", template, err, errText(res))
	}
	return out.ClaimID
}

// A step's description and its rationale are two signed claims about its foton, under two
// predicates, so a query by predicate tells them apart (ADR-005 "Settled on the way" 1). A second
// description is a second claim: the cockpit supersedes nothing on its own (SPEC §8.6).
func TestSay_DescribesAndRationaleAreTwoClaimsAboutAFoton(t *testing.T) {
	r := describesRepo(t)
	pub := publishOne(t, r)

	want := map[string]string{
		sayValue(t, pub.FotonID, "describes", "dropped rows without a visit date"):                 describesIRI,
		sayValue(t, pub.FotonID, "rationale", "the protocol counts visits, not patients"):          rationaleIRI,
		sayValue(t, pub.FotonID, "describes", "dropped rows without a visit date, and duplicates"): describesIRI,
	}

	ask, askOut, err := Ask(context.Background(), nil, AskRequest{Query: "about", Ref: pub.FotonID})
	if err != nil || ask.IsError {
		t.Fatalf("about query failed: err=%v result=%+v", err, errText(ask))
	}
	got := map[string]string{}
	for _, c := range askOut.Claims {
		if c.Predicate == describesIRI || c.Predicate == rationaleIRI {
			got[c.ID] = c.Predicate
		}
	}
	for id, pred := range want {
		if got[id] != pred {
			t.Errorf("claim %s: want predicate %s, got %q (all: %v)", id, pred, got[id], got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("want %d claims, got %v", len(want), got)
	}
}

func TestSay_DescribesRefusesAnEmptyClaim(t *testing.T) {
	r := describesRepo(t)
	pub := publishOne(t, r)

	for _, tmpl := range []string{"describes", "rationale"} {
		res, _, err := Say(context.Background(), nil, SayRequest{Subject: pub.FotonID, Template: tmpl})
		if err != nil {
			t.Fatalf("%s: expected a tool-level error, not a Go error: %v", tmpl, err)
		}
		if !res.IsError {
			t.Errorf("say accepted a %s claim without a value", tmpl)
		}
	}
}
