package cockpit

import (
	"context"
	"testing"
)

func feedAsk(t *testing.T, in AskRequest) *FeedPage {
	t.Helper()
	in.Query = "feed"
	res, out, err := Ask(context.Background(), nil, in)
	if err != nil || res.IsError {
		t.Fatalf("ask feed: %v %s", err, errText(res))
	}
	return out.Feed
}

func feedIDs(p *FeedPage) map[string]string {
	ids := map[string]string{}
	for _, r := range p.Records {
		ids[r.ID] = r.Kind
	}
	return ids
}

// Query feed is the union's trusted records after a cursor, each with the key that verifies it and
// the name the trust configuration gives that key, and the cursor to ask from next: nothing again
// when nothing moved, only the new record when one arrived, every record once across pages.
func TestFeed_TheRecordsAfterACursorWithTheirSigner(t *testing.T) {
	r := describesRepo(t)
	f1 := publishOne(t, r).FotonID
	c1 := sayValue(t, f1, "describes", "the first step")

	p := feedAsk(t, AskRequest{})
	if ids := feedIDs(p); len(ids) != 2 || ids[f1] != "foton" || ids[c1] != "claim" || p.More || p.Untrusted != 0 {
		t.Fatalf("from the start: want the foton and the claim: %+v", p)
	}
	for _, rec := range p.Records {
		if len(rec.Signers) != 1 || rec.Signers[0].KeyID == "" || rec.Signers[0].Tier == "" || rec.Signers[0].Name == "" || rec.Registry != "own" || len(rec.Envelope) == 0 {
			t.Errorf("want the verifying key, its tier and name, the registry and the envelope: %+v", rec)
		}
	}

	if again := feedAsk(t, AskRequest{Cursor: p.Cursor}); len(again.Records) != 0 {
		t.Fatalf("nothing moved: want no record: %+v", again)
	}

	c2 := sayValue(t, f1, "rationale", "why it was run")
	next := feedAsk(t, AskRequest{Cursor: p.Cursor})
	if ids := feedIDs(next); len(ids) != 1 || ids[c2] != "claim" {
		t.Fatalf("one new claim: want it alone: %+v", next)
	}

	seen := map[string]int{}
	cursor := ""
	for pages := 0; ; pages++ {
		pg := feedAsk(t, AskRequest{Cursor: cursor, Limit: 1})
		for _, rec := range pg.Records {
			seen[rec.ID]++
		}
		cursor = pg.Cursor
		if !pg.More {
			break
		}
		if pages > 10 {
			t.Fatal("paging does not end")
		}
	}
	if len(seen) != 3 || seen[f1] != 1 || seen[c1] != 1 || seen[c2] != 1 {
		t.Errorf("one record per page: want each of the three once: %v", seen)
	}
}

func TestFeed_RefusesWhatItDoesNotTake(t *testing.T) {
	r := describesRepo(t)
	publishOne(t, r)
	for name, in := range map[string]AskRequest{
		"ref":    {Query: "feed", Ref: "sha256:00"},
		"filter": {Query: "feed", Filter: &AskFilter{TrustTier: "self"}},
		"cursor": {Query: "feed", Cursor: "not-a-cursor"},
		"limit":  {Query: "feed", Limit: -1},
	} {
		if res, _, _ := Ask(context.Background(), nil, in); res == nil || !res.IsError {
			t.Errorf("%s: want a refusal", name)
		}
	}
}
