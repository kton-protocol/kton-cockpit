package watch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A tool asks the editor; with no window listening it hears so at once, with one it gets the
// window's answer, and an answer nobody waits for any more is refused.
func TestUIHub_ATripThroughTheEditor(t *testing.T) {
	sv := &Service{Store: t.TempDir()}
	sv.Load()
	srv := httptest.NewServer(sv.Handler())
	defer srv.Close()

	if _, err := sv.UI.Do(context.Background(), "open_story", nil); !errors.Is(err, ErrNoEditor) {
		t.Fatalf("no window: %v", err)
	}

	// a window: listens to the events and answers each
	res, err := http.Get(srv.URL + "/api/ui/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev UIEvent
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev)
			b, _ := json.Marshal(UIAnswer{ID: ev.ID, OK: true, Message: "opened " + ev.Args["path"].(string)})
			http.Post(srv.URL+"/api/ui/answer", "application/json", strings.NewReader(string(b)))
		}
	}()
	for i := 0; sv.UI.Listening() == 0 && i < 100; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, err := sv.UI.Do(ctx, "open_story", map[string]any{"path": "figures/gof.png"})
	if err != nil || !a.OK || a.Message != "opened figures/gof.png" {
		t.Fatalf("answer %+v, %v", a, err)
	}

	stale, _ := http.Post(srv.URL+"/api/ui/answer", "application/json", strings.NewReader(`{"id":"nobody","ok":true}`))
	if stale.StatusCode != http.StatusGone {
		t.Fatalf("an answer nobody waits for: %d", stale.StatusCode)
	}
}

func TestUIHub_AWindowThatDoesNotAnswerTimesOut(t *testing.T) {
	h := NewUIHub()
	c := h.subscribe(uiWindow{})
	defer h.unsubscribe(c)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := h.Do(ctx, "copy", nil); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("%v", err)
	}
	if len(h.waiting) != 0 {
		t.Fatal("the waiting answer was not cleared")
	}
}

// MH 12.11., mh4: the story Claude in Positron asked for opened in a VS Code window
func TestUITargets_TheWindowWithTheProjectAndPositronFirst(t *testing.T) {
	code, posi, other := make(chan UIEvent, 1), make(chan UIEvent, 1), make(chan UIEvent, 1)
	subs := map[chan UIEvent]uiWindow{
		code:  {App: "Visual Studio Code", Folders: []string{"/w/kton-coordination/surfaces/kunden"}},
		posi:  {App: "Positron", Folders: []string{"/g/kton-theo-pmx-mh4"}},
		other: {App: "Positron", Folders: []string{"/g/other"}},
	}
	one := func(args map[string]any) chan UIEvent {
		t.Helper()
		got := targets(subs, args)
		if len(got) != 1 {
			t.Fatalf("%v: %d windows", args, len(got))
		}
		return got[0]
	}
	if one(map[string]any{"project": "/g/kton-theo-pmx-mh4"}) != posi {
		t.Fatalf("the project's window")
	}
	if one(map[string]any{"path": "/w/kton-coordination/surfaces/kunden/x.md"}) != code {
		t.Fatalf("a project only VS Code has open goes there")
	}
	if got := targets(subs, map[string]any{}); len(got) != 2 || (got[0] == code || got[1] == code) {
		t.Fatalf("no project: the Positron windows, %d", len(got))
	}
	old := map[chan UIEvent]uiWindow{code: {}, posi: {}}
	if len(targets(old, map[string]any{"project": "/g/x"})) != 2 {
		t.Fatalf("windows that name nothing: all, as before")
	}
}
