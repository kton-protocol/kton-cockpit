package cockpit

import (
	"context"
	"testing"
)

// Where a record lives in improve is a signed claim, not carried material, so it survives a mirror
// (SPEC §8.8, ADR-015). located-at is said about a published foton; runs-on about a tool
// installation. Both refuse a claim without their required fields.
func TestSay_LocatedAtAndRunsOnAreClaims(t *testing.T) {
	r := describesRepo(t)
	raw := r.Config(t).Raw
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "located-at", "runs-on")
	r.WriteConfig(t, raw)
	pub := publishOne(t, r)
	ctx := context.Background()

	res, out, err := Say(ctx, nil, SayRequest{Subject: pub.FotonID, Template: "located-at", Fields: map[string]string{
		"server": "https://robert1.hc.scinteco.com/api", "step": "robert1.hc.scinteco.com-10110:ST-306627",
		"url": "https://robert1.hc.scinteco.com/e/ST-306627", "tree": "/projects/demo/analysis"}})
	if err != nil || res.IsError {
		t.Fatalf("say located-at: err=%v result=%+v", err, errText(res))
	}
	located := out.ClaimID

	res, out, err = Say(ctx, nil, SayRequest{Subject: located, Template: "runs-on", Fields: map[string]string{
		"server": "https://robert1.hc.scinteco.com/api", "runServer": "rs1", "instance": "nonmem_7.5 psn5.4.0 docker",
		"envRef": "oci://example/nonmem@" + pub.FotonID}})
	if err != nil || res.IsError {
		t.Fatalf("say runs-on about a claim: err=%v result=%+v", err, errText(res))
	}

	for tmpl, fields := range map[string]map[string]string{
		"located-at": {"server": "https://robert1.hc.scinteco.com/api"},
		"runs-on":    {"server": "s", "runServer": "rs1", "instance": "i"},
	} {
		res, _, err := Say(ctx, nil, SayRequest{Subject: pub.FotonID, Template: tmpl, Fields: fields})
		if err != nil {
			t.Fatalf("%s: want a tool-level refusal, got %v", tmpl, err)
		}
		if !res.IsError {
			t.Errorf("%s was accepted without a required field", tmpl)
		}
	}

	_, askOut, err := Ask(ctx, nil, AskRequest{Query: "about", Ref: pub.FotonID})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range askOut.Claims {
		if c.ID == located && c.Predicate == "https://kton.dev/v/located-at" {
			found = true
		}
	}
	if !found {
		t.Errorf("the located-at claim is not answered about the foton: %+v", askOut.Claims)
	}
}
