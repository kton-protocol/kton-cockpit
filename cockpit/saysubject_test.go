package cockpit

import (
	"context"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// A say request's subject is a record or claim id, an IRI, or a file in the repository, which stands
// for its bytes and is reported back — in every form of say, as the shell form always did. Anything
// else is refused rather than signed about a literal (SPEC §8; userinterface's v0.5.0 probe).
func TestSay_ASubjectThatIsAFileIsItsBytes(t *testing.T) {
	r := testrepo.New(t)
	r.Write(t, "data.txt", "a\n1\n")
	r.Use(t)
	pub := publishOne(t, r)

	res, out, err := Say(context.Background(), nil, SayRequest{Subject: "data.txt", Template: "working-on",
		Fields: map[string]string{"step": "checking"}})
	if err != nil || res.IsError {
		t.Fatalf("say about a file: %v %s", err, errText(res))
	}
	if !strings.HasPrefix(out.Subject, "sha256:") || out.ResolvedFrom != "data.txt" {
		t.Fatalf("want the file's bytes as subject, reported back: subject %q resolvedFrom %q", out.Subject, out.ResolvedFrom)
	}
	_, ans, err := Ask(context.Background(), nil, AskRequest{Query: "about", Ref: "data.txt"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range ans.Claims {
		found = found || c.ID == out.ClaimID
	}
	if !found {
		t.Fatalf("ask about data.txt does not find the claim made about data.txt: %+v", ans.Claims)
	}

	// An id in upper case is the same id; an IRI is taken as it is.
	for _, s := range []string{"sha256:" + strings.ToUpper(strings.TrimPrefix(pub.FotonID, "sha256:")), "urn:example:thing"} {
		if res, _, err := Say(context.Background(), nil, SayRequest{Subject: s, Template: "working-on",
			Fields: map[string]string{"step": "checking"}}); err != nil || res.IsError {
			t.Errorf("say about %s: %v %s", s, err, errText(res))
		}
	}
	for s, why := range map[string]string{
		"no-such-file.txt": "not a sha256: id, an IRI, or a file",
		"work":             "directory",
		"../outside.txt":   "outside this repository",
		"sha256:xyz":       "64 hex digits",
	} {
		if s == "work" {
			r.Write(t, "work/x", "x")
		}
		res, _, _ := Say(context.Background(), nil, SayRequest{Subject: s, Template: "working-on",
			Fields: map[string]string{"step": "checking"}})
		if !res.IsError || !strings.Contains(errText(res), why) {
			t.Errorf("say about %q: want refused with %q, got %s", s, why, errText(res))
		}
	}
}
