package cockpit

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/show"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// chainServer publishes in.csv → s1.csv → s2.csv → s3.csv as three fotons, claims about the middle
// one, and serves it. It returns the fotons in chain order, the claim, and each output's hash.
func chainServer(t *testing.T) (*httptest.Server, []string, string, []string) {
	t.Helper()
	r := testrepo.New(t)
	r.Use(t)
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	var fotons, hashes []string
	prev := "data/in.csv"
	for i := 1; i <= 3; i++ {
		out := fmt.Sprintf("data/s%d.csv", i)
		r.Write(t, out, fmt.Sprintf("id,step\n1,%d\n", i))
		res, pub, err := Publish(context.Background(), nil, PublishRequest{
			Inputs: []string{prev}, Outputs: []string{out}, Cmd: fmt.Sprintf("step%d %s > %s", i, prev, out)})
		if err != nil || res.IsError {
			t.Fatalf("publish step %d: err=%v %s", i, err, errText(res))
		}
		fotons = append(fotons, pub.FotonID)
		hashes = append(hashes, pub.OutputHashes[out])
		prev = out
	}
	claim := sayWorkingOn(t, fotons[1])
	h, err := show.New(r.Config(t)).Handler("")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, fotons, claim, hashes
}

func ids(sel show.Selection) []string {
	var out []string
	for _, rec := range sel.Records {
		if len(rec.Envelope) == 0 {
			panic("a selected record without its envelope: " + rec.FotonID + rec.ClaimID)
		}
		out = append(out, rec.FotonID+rec.ClaimID)
	}
	return out
}

func same(a, b []string) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

// A viewer asks for the part it draws: the pedigree of a hash N hops up, what built on it, or one
// node — each record with its envelope and the ring to verify it, and a cursor when the page is cut.
func TestShow_ASelectionIsThePortionAskedForWithEnvelopesAndACursor(t *testing.T) {
	ts, fotons, claim, hashes := chainServer(t)
	get := func(query string) show.Selection {
		var sel show.Selection
		getJSON(t, ts.URL+"/data/select?"+query, &sel)
		if len(sel.Keys) == 0 {
			t.Fatalf("%s: no keys to verify with", query)
		}
		return sel
	}

	// The pedigree of the last output: its producer, then one hop up, with the claim about step 2.
	if got, want := ids(get("q=pedigree&hash="+hashes[2]+"&hops=1")), []string{fotons[2], fotons[1], claim}; !same(got, want) {
		t.Errorf("pedigree, 1 hop: got %v want %v", got, want)
	}
	if got := ids(get("q=pedigree&hash=" + hashes[2] + "&hops=5")); len(got) != 4 {
		t.Errorf("pedigree, whole chain: want 3 fotons and the claim, got %v", got)
	}
	// What built on step 1's output: step 2 (and its claim), and with one more hop, step 3.
	if got, want := ids(get("q=uses&hash="+hashes[0]+"&hops=0")), []string{fotons[1], claim}; !same(got, want) {
		t.Errorf("uses, 0 hops: got %v want %v", got, want)
	}
	if got, want := ids(get("q=uses&hash="+hashes[0])), []string{fotons[1], claim, fotons[2]}; !same(got, want) {
		t.Errorf("uses, 1 hop: got %v want %v", got, want)
	}
	// One node, expanded: the foton and what was said about it.
	if got, want := ids(get("q=record&id="+fotons[1])), []string{fotons[1], claim}; !same(got, want) {
		t.Errorf("record: got %v want %v", got, want)
	}

	// Paged: two at a time, the cursor carries on where the page stopped, and the pages add up.
	first := get("q=pedigree&hash=" + hashes[2] + "&hops=5&limit=2")
	if len(first.Records) != 2 || first.More == "" {
		t.Fatalf("first page: want 2 records and a cursor, got %v more=%q", ids(first), first.More)
	}
	second := get("q=pedigree&hash=" + hashes[2] + "&hops=5&limit=2&cursor=" + first.More)
	if len(second.Records) != 2 || second.More != "" {
		t.Fatalf("second page: want the last 2 and no cursor, got %v more=%q", ids(second), second.More)
	}
	if all := append(ids(first), ids(second)...); !same(all, ids(get("q=pedigree&hash="+hashes[2]+"&hops=5"))) {
		t.Errorf("the pages do not add up to the whole selection: %v", all)
	}

	// A malformed selection is the caller's mistake, said as such.
	resp, err := http.Get(ts.URL + "/data/select?q=everything")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown q: want 400, got %s %s", resp.Status, b)
	}
}

// A scope is selected as its seed and its chain, in the order the store received them.
func TestShow_ASelectedScopeIsItsSeedAndChain(t *testing.T) {
	r, scope := scopedRepo(t)
	pub := publishOne(t, r)
	c1 := sayInScope(t, pub.FotonID, "review")
	c2 := sayInScope(t, pub.FotonID, "review")
	h, err := show.New(r.Config(t)).Handler("")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	var sel show.Selection
	getJSON(t, ts.URL+"/data/select?q=scope&id="+scope, &sel)
	if got, want := ids(sel), []string{scope, c1, c2}; !same(got, want) {
		t.Errorf("scope: got %v want %v", got, want)
	}
}
