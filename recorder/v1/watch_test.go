package v1

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

type event struct {
	name string
	data json.RawMessage
}

// stream reads the events of a /v1/watch answer into a channel until the body ends.
func stream(t *testing.T, srv *httptest.Server, query, token string) (<-chan event, *http.Response) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/v1/watch?"+query, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan event, 16)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		var ev event
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = json.RawMessage(strings.TrimPrefix(line, "data: "))
			case line == "" && ev.name != "":
				ch <- ev
				ev = event{}
			}
		}
	}()
	return ch, resp
}

func next(t *testing.T, ch <-chan event) event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("the stream ended")
		}
		return ev
	case <-time.After(20 * time.Second):
		t.Fatal("no event")
	}
	return event{}
}

// The workgroup extension (coordination 02.11.) and mignon (handoff 2.2) are told about new
// records instead of walking the registry: from the end, a claim said while watching arrives with
// the key that verifies it, and a client that reconnects with its cursor misses nothing.
func TestWatch_NewRecordsArriveWithTheirSignerFromTheEnd(t *testing.T) {
	r := testrepo.New(t)
	raw := r.Config(t).Raw
	raw.Claims.AllowedTemplates = append(raw.Claims.AllowedTemplates, "describes")
	r.WriteConfig(t, raw)
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	r.Write(t, "data/out.csv", "id,result\n1,84\n")
	ctx := context.Background()
	c := cockpit.New(cockpit.Start{Dir: r.Root})
	pub, err := c.Publish(ctx, cockpit.PublishRequest{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"}, Cmd: "double data/in.csv"})
	if err != nil || pub.FotonID == "" {
		t.Fatalf("publish: %v %+v", err, pub)
	}

	s := &Server{Token: "t0ken", Every: 50 * time.Millisecond}
	defer s.Close()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	if _, resp := stream(t, srv, "repo="+url.QueryEscape(r.Root), "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without the token: %d", resp.StatusCode)
	}

	ch, resp := stream(t, srv, "since=end&repo="+url.QueryEscape(r.Root), "t0ken")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("watch: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	ev := next(t, ch)
	var cur struct{ Cursor string }
	if json.Unmarshal(ev.data, &cur); ev.name != EventCursor || cur.Cursor == "" {
		t.Fatalf("since=end: want the cursor first, not the foton already there: %s %s", ev.name, ev.data)
	}

	said, err := c.Say(ctx, cockpit.SayRequest{Subject: pub.FotonID, Template: "describes", Fields: map[string]string{"value": "doubles the values"}})
	if err != nil || said.ClaimID == "" {
		t.Fatalf("say: %v %+v", err, said)
	}
	ev = next(t, ch)
	var rec cockpit.FeedRecord
	if json.Unmarshal(ev.data, &rec); ev.name != EventRecord || rec.ID != said.ClaimID || rec.Kind != "claim" || len(rec.Signers) != 1 || rec.Signers[0].Name == "" {
		t.Fatalf("want the new claim with its signer: %s %s", ev.name, ev.data)
	}
	if ev = next(t, ch); ev.name != EventCursor {
		t.Fatalf("after the record its cursor: %s", ev.name)
	}
	resp.Body.Close()

	// from the first cursor again: the claim, once, and nothing older
	ch, resp = stream(t, srv, "since="+url.QueryEscape(cur.Cursor)+"&repo="+url.QueryEscape(r.Root), "t0ken")
	defer resp.Body.Close()
	ev = next(t, ch)
	if json.Unmarshal(ev.data, &rec); ev.name != EventRecord || rec.ID != said.ClaimID {
		t.Fatalf("reconnecting with the cursor: want the claim: %s %s", ev.name, ev.data)
	}
	if ev = next(t, ch); ev.name != EventCursor {
		t.Fatalf("then the cursor, nothing else: %s %s", ev.name, ev.data)
	}
}
