// Package frontrpc is the frontend protocol's wire: JSON-RPC 2.0 over a byte stream, framed as a
// language server frames it (a Content-Length header, a blank line, the JSON body), so an editor
// extension can use the JSON-RPC library it already has (ADR-011, section 6).
//
// It is a transport and nothing else: it reads requests, runs each in its own goroutine with a
// context the client can cancel, writes the answers and the notifications one at a time, and maps
// nothing. What a method does, and how its errors read, is the caller's.
package frontrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// The JSON-RPC 2.0 and language-server error codes this package answers with itself.
const (
	CodeParseError       = -32700
	CodeInvalidRequest   = -32600
	CodeMethodNotFound   = -32601
	CodeInvalidParams    = -32602
	CodeInternalError    = -32603
	CodeRequestCancelled = -32800
)

// Error is a JSON-RPC error object. A handler that returns one has it sent as it is.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Handler answers one method. params is the request's params, as sent (null when absent).
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// Server reads requests from r and writes answers and notifications to w.
type Server struct {
	r *bufio.Reader
	w io.Writer

	wmu sync.Mutex // one message at a time on w

	mu       sync.Mutex
	handlers map[string]Handler
	inline   map[string]bool
	inflight map[string]context.CancelFunc
	closing  bool           // shutdown came: no new request is taken
	wg       sync.WaitGroup // the requests running alongside the reader

	// MapError turns a handler's error into the error object sent. Nil sends CodeInternalError with
	// the error's text.
	MapError func(error) *Error
	// OnNotification is called for a notification other than $/cancelRequest (exit, for one).
	OnNotification func(method string, params json.RawMessage)
}

// NewServer returns a server over r and w, with no methods yet.
func NewServer(r io.Reader, w io.Writer) *Server {
	return &Server{r: bufio.NewReader(r), w: w, handlers: map[string]Handler{}, inline: map[string]bool{}, inflight: map[string]context.CancelFunc{}}
}

// HandleInline registers h for method, run before the next message is read rather than alongside the
// others: what a client sends after it (say, right after initialize) sees what it did.
func (s *Server) HandleInline(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
	s.inline[method] = true
}

// Handle registers h for method.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Serve reads until r ends, then waits for the requests still running: every request it read is
// answered before it returns. Cancelling ctx cancels them.
func (s *Server) Serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		s.wg.Wait()
		cancel()
	}()
	for {
		body, err := ReadMessage(s.r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		var m message
		if err := json.Unmarshal(body, &m); err != nil {
			s.reply(nil, nil, &Error{Code: CodeParseError, Message: "not JSON: " + err.Error()})
			continue
		}
		if m.JSONRPC != "2.0" || m.Method == "" {
			s.reply(m.ID, nil, &Error{Code: CodeInvalidRequest, Message: `a request needs "jsonrpc": "2.0" and a method`})
			continue
		}
		if len(m.ID) == 0 || string(m.ID) == "null" {
			if m.Method == "exit" {
				return nil // the deferred wait answers what is still running first
			}
			s.notification(m.Method, m.Params)
			continue
		}
		s.request(ctx, m)
	}
}

func (s *Server) notification(method string, params json.RawMessage) {
	if method == "$/cancelRequest" {
		var p struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal(params, &p) == nil {
			s.mu.Lock()
			if cancel := s.inflight[idKey(p.ID)]; cancel != nil {
				cancel()
			}
			s.mu.Unlock()
		}
		return
	}
	if s.OnNotification != nil {
		s.OnNotification(method, params)
	}
}

func (s *Server) request(ctx context.Context, m message) {
	s.mu.Lock()
	h := s.handlers[m.Method]
	inline := s.inline[m.Method]
	closing := s.closing
	key := idKey(m.ID)
	_, busy := s.inflight[key]
	var rctx context.Context
	var cancel context.CancelFunc
	if h != nil && !busy && !closing {
		rctx, cancel = context.WithCancel(ctx)
		s.inflight[key] = cancel
	}
	s.mu.Unlock()
	switch {
	case h == nil:
		s.reply(m.ID, nil, &Error{Code: CodeMethodNotFound, Message: "no method " + strconv.Quote(m.Method)})
		return
	case closing:
		s.reply(m.ID, nil, &Error{Code: CodeInvalidRequest, Message: "the connection is shutting down: no new request is taken"})
		return
	case busy:
		s.reply(m.ID, nil, &Error{Code: CodeInvalidRequest, Message: "a request with id " + string(m.ID) + " is still running"})
		return
	}
	run := func() {
		defer func() {
			s.mu.Lock()
			delete(s.inflight, key)
			s.mu.Unlock()
			cancel()
		}()
		params := m.Params
		if len(params) == 0 {
			params = json.RawMessage("null")
		}
		result, err := s.call(rctx, h, params)
		if err == nil {
			s.reply(m.ID, result, nil)
			return
		}
		if rctx.Err() != nil {
			s.reply(m.ID, nil, &Error{Code: CodeRequestCancelled, Message: "cancelled: " + err.Error()})
			return
		}
		var e *Error
		switch {
		case errors.As(err, &e):
		case s.MapError != nil:
			e = s.MapError(err)
		default:
			e = &Error{Code: CodeInternalError, Message: err.Error()}
		}
		s.reply(m.ID, nil, e)
	}
	if inline {
		run()
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		run()
	}()
}

// Drain takes no new request and waits until every request running alongside the reader is answered.
// A handler registered inline calls it to shut down: every request accepted before is answered first.
func (s *Server) Drain() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	s.wg.Wait()
}

// idKey is the key a request is cancelled by: a number by its value (1 and 1.0 are one id), a string
// by its text. JSON-RPC keeps the number 1 and the string "1" apart, and so does this.
func idKey(id json.RawMessage) string {
	var v any
	if json.Unmarshal(id, &v) != nil {
		return "raw:" + string(id)
	}
	switch x := v.(type) {
	case float64:
		return "n:" + strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return "s:" + x
	}
	return "raw:" + string(id)
}

// call runs h, turning a panic into an error so one bad request does not end the connection.
func (s *Server) call(ctx context.Context, h Handler, params json.RawMessage) (result any, err error) {
	defer func() {
		if p := recover(); p != nil {
			result, err = nil, &Error{Code: CodeInternalError, Message: fmt.Sprintf("the handler failed: %v", p)}
		}
	}()
	return h(ctx, params)
}

func (s *Server) reply(id json.RawMessage, result any, e *Error) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	out := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		out["error"] = e
	} else {
		out["result"] = result
	}
	_ = s.write(out)
}

// Notify sends a notification to the client.
func (s *Server) Notify(method string, params any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *Server) write(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	return WriteMessage(s.w, body)
}

// WriteMessage writes one framed message.
func WriteMessage(w io.Writer, body []byte) error {
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

// maxMessage bounds one message, so a wrong length cannot make the reader allocate without limit.
const maxMessage = 256 << 20

// ReadMessage reads one framed message: headers up to a blank line, of which Content-Length is
// required and Content-Type is ignored, then that many bytes.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && line == "" && length < 0 {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("reading a header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("not a header: %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 || n > maxMessage {
				return nil, fmt.Errorf("Content-Length %q", strings.TrimSpace(value))
			}
			length = n
		}
	}
	if length < 0 {
		return nil, errors.New("a message without Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("reading %d bytes: %w", length, err)
	}
	return body, nil
}
