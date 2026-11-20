package frontrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// client talks to a Server over two pipes, as an editor extension does over stdio.
type client struct {
	w   io.Writer
	r   *bufio.Reader
	end func()
}

func start(t *testing.T, setup func(*Server)) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := NewServer(inR, outW)
	setup(s)
	done := make(chan struct{})
	go func() {
		_ = s.Serve(context.Background())
		outW.Close()
		close(done)
	}()
	c := &client{w: inW, r: bufio.NewReader(outR), end: func() { inW.Close(); <-done }}
	t.Cleanup(func() { inW.Close() })
	return c
}

func (c *client) send(t *testing.T, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := WriteMessage(c.w, b); err != nil {
		t.Fatal(err)
	}
}

type reply struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

func (c *client) read(t *testing.T) reply {
	t.Helper()
	b, err := ReadMessage(c.r)
	if err != nil {
		t.Fatal(err)
	}
	var r reply
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return r
}

func req(id any, method string, params any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}

// A request is answered under its id; an unknown method, a malformed request and a handler's error are
// answered as errors, and the connection goes on.
func TestServer_AnswersRequestsAndErrorsAndGoesOn(t *testing.T) {
	c := start(t, func(s *Server) {
		s.Handle("echo", func(_ context.Context, p json.RawMessage) (any, error) { return json.RawMessage(p), nil })
		s.Handle("fail", func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("no") })
		s.Handle("panic", func(context.Context, json.RawMessage) (any, error) { panic("boom") })
		s.MapError = func(err error) *Error { return &Error{Code: -32001, Message: err.Error(), Data: "mapped"} }
	})
	c.send(t, req(1, "echo", map[string]int{"a": 1}))
	if r := c.read(t); string(r.ID) != "1" || string(r.Result) != `{"a":1}` {
		t.Errorf("echo: %+v", r)
	}
	c.send(t, req("x", "nothing", nil))
	if r := c.read(t); r.Error == nil || r.Error.Code != CodeMethodNotFound || string(r.ID) != `"x"` {
		t.Errorf("unknown method: %+v", r)
	}
	c.send(t, map[string]any{"id": 2, "method": "echo"})
	if r := c.read(t); r.Error == nil || r.Error.Code != CodeInvalidRequest {
		t.Errorf("no jsonrpc 2.0: %+v", r)
	}
	c.send(t, req(3, "fail", nil))
	if r := c.read(t); r.Error == nil || r.Error.Code != -32001 || r.Error.Data != "mapped" {
		t.Errorf("a handler's error goes through MapError: %+v", r)
	}
	c.send(t, req(4, "panic", nil))
	if r := c.read(t); r.Error == nil || r.Error.Code != CodeInternalError {
		t.Errorf("a panicking handler: %+v", r)
	}
	c.send(t, req(5, "echo", "still here"))
	if r := c.read(t); string(r.Result) != `"still here"` {
		t.Errorf("after errors the connection goes on: %+v", r)
	}
}

// $/cancelRequest cancels a running request, which is answered RequestCancelled; another request runs
// alongside it meanwhile.
func TestServer_CancelsARunningRequest(t *testing.T) {
	started := make(chan struct{})
	c := start(t, func(s *Server) {
		s.Handle("wait", func(ctx context.Context, _ json.RawMessage) (any, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		s.Handle("echo", func(_ context.Context, p json.RawMessage) (any, error) { return json.RawMessage(p), nil })
	})
	c.send(t, req(7, "wait", nil))
	<-started
	c.send(t, req(8, "echo", "alongside"))
	if r := c.read(t); string(r.ID) != "8" {
		t.Fatalf("another request should answer while one waits: %+v", r)
	}
	c.send(t, map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": 7}})
	if r := c.read(t); string(r.ID) != "7" || r.Error == nil || r.Error.Code != CodeRequestCancelled {
		t.Errorf("cancelled: %+v", r)
	}
}

// When the client closes its end, the requests still running are answered before Serve returns.
func TestServer_AnswersWhatIsRunningWhenTheInputEnds(t *testing.T) {
	release := make(chan struct{})
	c := start(t, func(s *Server) {
		s.Handle("slow", func(ctx context.Context, _ json.RawMessage) (any, error) {
			<-release
			return "done", ctx.Err()
		})
	})
	c.send(t, req(1, "slow", nil))
	go func() { time.Sleep(50 * time.Millisecond); close(release) }()
	go c.end()
	if r := c.read(t); string(r.Result) != `"done"` {
		t.Errorf("a request running when the input ended: %+v", r)
	}
}

// Notifications go out framed like answers.
func TestServer_SendsNotifications(t *testing.T) {
	var s *Server
	c := start(t, func(x *Server) { s = x })
	sent := make(chan error, 1)
	go func() { sent <- s.Notify("said", map[string]string{"claim": "sha256:1"}) }() // the pipe has no buffer
	if r := c.read(t); r.Method != "said" || len(r.ID) != 0 {
		t.Errorf("notification: %+v", r)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
}

// The framing: Content-Length is required and bounded; other headers are ignored.
func TestReadMessage_Framing(t *testing.T) {
	read := func(s string) ([]byte, error) { return ReadMessage(bufio.NewReader(strings.NewReader(s))) }
	if b, err := read("Content-Type: application/vscode-jsonrpc; charset=utf-8\r\nContent-Length: 2\r\n\r\n{}"); err != nil || string(b) != "{}" {
		t.Errorf("with Content-Type: %q %v", b, err)
	}
	for _, bad := range []string{"\r\n{}", "Content-Length: x\r\n\r\n", "Content-Length: 999999999999\r\n\r\n", "garbage\r\n\r\n", "Content-Length: 5\r\n\r\n{}"} {
		if _, err := read(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	if _, err := read(""); !errors.Is(err, io.EOF) {
		t.Errorf("an empty stream is EOF: %v", err)
	}
}
