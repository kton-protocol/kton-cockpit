package watch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// The editor's part of what an assistant does through the MCP tools: opening a story or the graph,
// selecting steps, copying a file over. ktonwatch cannot do any of it; the extension in each
// editor window can. So a tool posts an event here, the windows listening (GET /api/ui/events,
// server-sent events) get it, one of them carries it out (asking the person where it must) and
// answers (POST /api/ui/answer), and the tool reports what happened.

// UIEvent is one thing the editor is asked to do.
type UIEvent struct {
	ID     string         `json:"id"`
	Action string         `json:"action"` // open_story | open_graph | select | get_selection | copy
	Args   map[string]any `json:"args"`
}

// UIAnswer is what the editor did.
type UIAnswer struct {
	ID      string         `json:"id"`
	OK      bool           `json:"ok"`
	Message string         `json:"message,omitempty"` // in the person's words: "opened", "declined: …"
	Data    map[string]any `json:"data,omitempty"`    // e.g. the selection
}

// ErrNoEditor: no editor window is listening.
var ErrNoEditor = errors.New("no Positron window with the kton recorder is listening (open Positron, or reload its window)")

// UIHub passes events to the listening windows and their answers back.
type UIHub struct {
	mu      sync.Mutex
	subs    map[chan UIEvent]bool
	waiting map[string]chan UIAnswer
}

func NewUIHub() *UIHub {
	return &UIHub{subs: map[chan UIEvent]bool{}, waiting: map[string]chan UIAnswer{}}
}

// Listening is how many windows are subscribed.
func (h *UIHub) Listening() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Do sends an event to every listening window and waits for the first answer.
func (h *UIHub) Do(ctx context.Context, action string, args map[string]any) (UIAnswer, error) {
	b := make([]byte, 8)
	rand.Read(b)
	ev := UIEvent{ID: hex.EncodeToString(b), Action: action, Args: args}
	ans := make(chan UIAnswer, 1)
	h.mu.Lock()
	if len(h.subs) == 0 {
		h.mu.Unlock()
		return UIAnswer{}, ErrNoEditor
	}
	h.waiting[ev.ID] = ans
	for c := range h.subs {
		select {
		case c <- ev:
		default: // a window that does not read is skipped, not waited for
		}
	}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.waiting, ev.ID)
		h.mu.Unlock()
	}()
	select {
	case a := <-ans:
		return a, nil
	case <-ctx.Done():
		return UIAnswer{}, fmt.Errorf("the editor did not answer: %w", ctx.Err())
	}
}

// answer hands an editor's answer to the tool waiting for it; false when none waits (any more).
func (h *UIHub) answer(a UIAnswer) bool {
	h.mu.Lock()
	c, ok := h.waiting[a.ID]
	delete(h.waiting, a.ID)
	h.mu.Unlock()
	if ok {
		c <- a
	}
	return ok
}

func (h *UIHub) subscribe() chan UIEvent {
	c := make(chan UIEvent, 8)
	h.mu.Lock()
	h.subs[c] = true
	h.mu.Unlock()
	return c
}

func (h *UIHub) unsubscribe(c chan UIEvent) {
	h.mu.Lock()
	delete(h.subs, c)
	h.mu.Unlock()
}

// serveEvents: GET /api/ui/events, a stream of events for one editor window.
func (h *UIHub) serveEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	c := h.subscribe()
	defer h.unsubscribe(c)
	fmt.Fprint(w, ": kton ui\n\n")
	fl.Flush()
	tick := time.NewTicker(25 * time.Second) // keeps proxies and the client's timeout from closing it
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-c:
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		}
	}
}

// serveAnswer: POST /api/ui/answer {id, ok, message, data}.
func (h *UIHub) serveAnswer(w http.ResponseWriter, r *http.Request) {
	var a UIAnswer
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&a); err != nil || a.ID == "" {
		http.Error(w, "want {id, ok, message, data}", http.StatusBadRequest)
		return
	}
	if !h.answer(a) {
		http.Error(w, "no tool waits for "+a.ID, http.StatusGone)
		return
	}
	w.Write([]byte("{}"))
}
