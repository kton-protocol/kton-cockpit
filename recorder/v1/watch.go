// Package v1 is ktonwatch's service API for applications (mignon's handoff 2.1/2.2, the workgroup
// extension): HTTP/JSON on the cockpit's held union, behind the same token as the MCP door.
//
//	GET /v1/watch?repo=DIR[&since=CURSOR|end]   server-sent events: the union's new records
//
// A record comes with its verdict (the trusted keys that verify it, with tier and name), as the
// cockpit's query "feed" gives it (SPEC §9.10), so no client checks a signature itself.
package v1

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/kton-protocol/kton-cockpit/cockpit"
)

// Server serves /v1/. One held Cockpit per repository, shared by every watcher of it.
type Server struct {
	Token string
	// Every is how often a watch asks the feed for what is new (default 1 s); a registry that
	// did not move is not opened, so asking is cheap.
	Every time.Duration
	// Ping is how often an idle stream sends a comment, so proxies and clients keep it (default 25 s).
	Ping time.Duration

	mu   sync.Mutex
	held map[string]*cockpit.Cockpit
}

// Handler serves /v1/ for requests that carry the token.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/watch", s.watch)
	want := []byte("Bearer " + s.Token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// a loopback port is open to every process and every web page on the machine
		if s.Token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "kton /v1: a bearer token is needed (~/.config/kton/mcp-token)", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// Close lets every held union go.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, c := range s.held {
		c.Close()
		delete(s.held, k)
	}
}

func (s *Server) cockpit(ctx context.Context, repo string) (*cockpit.Cockpit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.held[repo]; ok {
		return c, nil
	}
	c, err := cockpit.Open(ctx, cockpit.Start{Dir: repo})
	if err != nil {
		return nil, err
	}
	if s.held == nil {
		s.held = map[string]*cockpit.Cockpit{}
	}
	s.held[repo] = c
	return c, nil
}

// Event names on the stream.
const (
	EventRecord = "record" // data: a cockpit.FeedRecord
	EventCursor = "cursor" // data: {"cursor", "untrusted"}: everything up to here is sent
	EventError  = "error"  // data: {"error"}: the watch stops
)

// watch streams the feed. since=end skips what is there already: the client learns its cursor
// and then only what arrives. A client that reconnects passes the last cursor it got.
func (s *Server) watch(w http.ResponseWriter, r *http.Request) {
	repo, since := r.URL.Query().Get("repo"), r.URL.Query().Get("since")
	if repo == "" {
		http.Error(w, "repo: the cockpit repository to watch", http.StatusBadRequest)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ctx := r.Context()
	c, err := s.cockpit(ctx, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	send := func(event string, v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	every, ping := s.Every, s.Ping
	if every <= 0 {
		every = time.Second
	}
	if ping <= 0 {
		ping = 25 * time.Second
	}

	cursor, skip := since, since == "end"
	if skip {
		cursor = ""
	}
	lastSent := time.Now()
	first := true
	for {
		res, err := c.Ask(ctx, cockpit.AskRequest{Query: "feed", Cursor: cursor})
		if ctx.Err() != nil {
			return
		}
		if err != nil || res.Feed == nil {
			msg := "no feed in the answer"
			if err != nil {
				msg = err.Error()
			}
			send(EventError, map[string]string{"error": msg})
			return
		}
		page := res.Feed
		if !skip {
			for _, rec := range page.Records {
				if !send(EventRecord, rec) {
					return
				}
			}
		}
		moved := page.Cursor != cursor
		cursor = page.Cursor
		if page.More {
			continue // the rest at once
		}
		skip = false
		if first || moved {
			if !send(EventCursor, map[string]any{"cursor": cursor, "untrusted": page.Untrusted}) {
				return
			}
			lastSent, first = time.Now(), false
		}
		if time.Since(lastSent) >= ping {
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
			lastSent = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
