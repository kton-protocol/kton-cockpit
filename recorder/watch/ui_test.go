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
	c := h.subscribe()
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
