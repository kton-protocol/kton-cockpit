package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/frontrpc"
)

// The frontend protocol (ADR-011, section 6; SPEC §12.5): `cockpit serve --stdio` answers JSON-RPC 2.0
// over stdin and stdout, framed as a language server frames it, for an editor extension or a viewer.
// Every method is one the command line has; each reaches the same implementation. A connection serves one
// workspace folder, named by initialize, and the binding guard runs on every request against it.

// ServeProtocol is the frontend protocol's version, in initialize's answer.
const ServeProtocol = 1

// Codes of the JSON-RPC error a refusal and a failure are sent as. data carries the CLI's JSON
// refusal, {code, clause, reason}, so a frontend branches on the same codes over both transports.
const (
	serveCodeRefusal        = -32001
	serveCodeFailure        = -32000
	serveCodeNotInitialized = -32002
)

// ServeInit is initialize's params.
type ServeInit struct {
	// Root is the workspace folder this connection serves: the repository, or a folder inside it.
	Root string `json:"root"`
	// As is the identity every say over this connection signs as (SPEC §8.7), as `mcp --as`: checked
	// here, and a say naming another is refused. Empty lets each say name any identity of the keyring,
	// or none for the checkout's own, as `mcp` without --as.
	As string `json:"as,omitempty"`
	// RequestTimeoutMs ends any request still running after that many milliseconds (0: none). A
	// client cancels one request with $/cancelRequest.
	RequestTimeoutMs int `json:"requestTimeoutMs,omitempty"`
}

// ServeInitResult is initialize's answer.
type ServeInitResult struct {
	Protocol int    `json:"protocol"`
	Cockpit  Build  `json:"cockpit"`
	Root     string `json:"root"`
	// RepoRoot is the repository the folder belongs to, the one every request answers for.
	RepoRoot string   `json:"repoRoot"`
	As       string   `json:"as,omitempty"`
	Methods  []string `json:"methods"`
}

// UICatParams is ui/cat's params: a package by name, dir or id, and the members' hashes.
type UICatParams struct {
	Package string   `json:"package"`
	Hashes  []string `json:"hashes"`
}

type serveSession struct {
	mu      sync.Mutex
	init    *ServeInit
	root    string
	timeout time.Duration
}

func (s *serveSession) get() (*ServeInit, string, time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.init, s.root, s.timeout, s.init != nil
}

func runServe(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] != "--stdio" {
		return fmt.Errorf("usage: cockpit serve --stdio   (the frontend protocol, JSON-RPC 2.0 over stdin and stdout)")
	}
	// stdout carries the protocol and nothing else: whatever else would print there goes to stderr.
	out := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = out }()
	return serve(ctx, os.Stdin, out)
}

// serve answers the frontend protocol on r and w until r ends or exit arrives.
func serve(ctx context.Context, r interface{ Read([]byte) (int, error) }, w interface{ Write([]byte) (int, error) }) error {
	srv := frontrpc.NewServer(r, w)
	srv.MapError = serveError
	sess := &serveSession{}
	// shutdown takes no new request and answers once every request it accepted before is answered;
	// exit and the end of the input end the connection after the same wait (SPEC §12.5).
	srv.HandleInline("shutdown", func(context.Context, json.RawMessage) (any, error) {
		srv.Drain()
		return nil, nil
	})
	for name, h := range serveMethods(sess) {
		if name == "initialize" {
			srv.HandleInline(name, h) // what follows it runs in the workspace it names
			continue
		}
		srv.Handle(name, h)
	}
	return srv.Serve(ctx)
}

// serveError sends a refusal as the CLI's JSON refusal, and any other error as code "error".
func serveError(err error) *frontrpc.Error {
	var r *cockpit.Refusal
	if errors.As(err, &r) {
		return &frontrpc.Error{Code: serveCodeRefusal, Message: r.Reason, Data: r}
	}
	return &frontrpc.Error{Code: serveCodeFailure, Message: err.Error(),
		Data: &cockpit.Refusal{Code: "error", Reason: err.Error()}}
}

// method adapts a function of typed params to a handler: params decoded strictly, the session
// initialized, the configuration loaded (the binding guard) and the timeout applied.
func method[P any, R any](sess *serveSession, fn func(ctx context.Context, sess *ServeInit, cfg *config.Config, p P) (R, error)) frontrpc.Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		in, root, timeout, ok := sess.get()
		if !ok {
			return nil, &frontrpc.Error{Code: serveCodeNotInitialized, Message: "initialize first: it names the workspace folder this connection serves"}
		}
		var p P
		if len(raw) > 0 && string(raw) != "null" {
			if err := strictJSON(raw, &p); err != nil {
				return nil, &frontrpc.Error{Code: frontrpc.CodeInvalidParams, Message: err.Error()}
			}
		}
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		cfg, err := config.Load(ctx, root)
		if err != nil {
			return nil, &cockpit.Refusal{Code: "binding", Clause: "SPEC §5", Reason: err.Error()}
		}
		return fn(ctx, in, cfg, p)
	}
}

func strictJSON(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

type none struct{}

func serveMethods(sess *serveSession) map[string]frontrpc.Handler {
	m := map[string]frontrpc.Handler{}
	c := func(root string) *cockpit.Cockpit { return cockpit.New(cockpit.Start{Dir: root}) }

	m["initialize"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in ServeInit
		if err := strictJSON(raw, &in); err != nil {
			return nil, &frontrpc.Error{Code: frontrpc.CodeInvalidParams, Message: err.Error()}
		}
		if in.Root == "" {
			return nil, &frontrpc.Error{Code: frontrpc.CodeInvalidParams, Message: "initialize needs root, the workspace folder"}
		}
		root, err := filepath.Abs(in.Root)
		if err != nil {
			return nil, err
		}
		// COCKPIT_REPO_DIR replaces the folder a configuration is looked for from (SPEC §5.1). A
		// connection serves the folder its window names, so one that the environment would redirect
		// elsewhere is refused rather than silently served from another repository.
		if env := os.Getenv("COCKPIT_REPO_DIR"); env != "" {
			if e, _ := filepath.Abs(env); e != root {
				return nil, &cockpit.Refusal{Code: "binding", Clause: "SPEC §5.1",
					Reason: fmt.Sprintf("COCKPIT_REPO_DIR is %s, but this connection was asked to serve %s: unset it for serve", env, root)}
			}
		}
		cfg, err := config.Load(ctx, root)
		if err != nil {
			return nil, &cockpit.Refusal{Code: "binding", Clause: "SPEC §5", Reason: err.Error()}
		}
		if in.As != "" {
			if err := cockpit.CheckIdentity(cfg, in.As); err != nil {
				return nil, err
			}
		}
		sess.mu.Lock()
		defer sess.mu.Unlock()
		if sess.init != nil {
			return nil, &frontrpc.Error{Code: frontrpc.CodeInvalidRequest, Message: "this connection is initialized already; a window opens its own"}
		}
		sess.init, sess.root = &in, root
		sess.timeout = time.Duration(in.RequestTimeoutMs) * time.Millisecond
		names := make([]string, 0, len(m))
		for k := range m {
			names = append(names, k)
		}
		sort.Strings(names)
		return ServeInitResult{Protocol: ServeProtocol, Cockpit: versionInfo(), Root: root, RepoRoot: cfg.RepoRoot, As: in.As, Methods: names}, nil
	}

	// The verbs, with the same request and result types as the Go API, MCP and the JSON argument.
	m["publish"] = method(sess, func(ctx context.Context, _ *ServeInit, cfg *config.Config, in cockpit.PublishRequest) (*cockpit.PublishResult, error) {
		return c(cfg.RepoRoot).Publish(ctx, in)
	})
	m["say"] = method(sess, func(ctx context.Context, s *ServeInit, cfg *config.Config, in cockpit.SayRequest) (*cockpit.SayResult, error) {
		if s.As != "" {
			if in.As != "" && in.As != s.As {
				return nil, &cockpit.Refusal{Code: "identity.as", Clause: "SPEC §8.7",
					Reason: fmt.Sprintf("this connection signs as %q, not %q", s.As, in.As)}
			}
			in.As = s.As
		}
		return c(cfg.RepoRoot).Say(ctx, in)
	})
	m["ask"] = method(sess, func(ctx context.Context, _ *ServeInit, cfg *config.Config, in cockpit.AskRequest) (*cockpit.AskResult, error) {
		return c(cfg.RepoRoot).Ask(ctx, in)
	})

	// What the command line lists under --json, by the same functions.
	m["say/templates"] = method(sess, func(_ context.Context, _ *ServeInit, cfg *config.Config, _ none) ([]Template, error) {
		return sayListing(cfg)
	})
	m["ask/questions"] = method(sess, func(_ context.Context, _ *ServeInit, cfg *config.Config, _ none) ([]Question, error) {
		return askListing(cfg), nil
	})
	m["ui/list"] = method(sess, func(_ context.Context, _ *ServeInit, cfg *config.Config, _ none) (UIListing, error) {
		return uiListing(cfg)
	})
	m["ui/cat"] = method(sess, func(_ context.Context, _ *ServeInit, cfg *config.Config, p UICatParams) (UICat, error) {
		return uiCatOf(cfg, p.Package, p.Hashes)
	})
	m["show/snapshot"] = method(sess, func(ctx context.Context, _ *ServeInit, cfg *config.Config, _ none) (ShowSnapshot, error) {
		return showSnapshot(ctx, cfg)
	})
	m["doctor"] = method(sess, func(ctx context.Context, _ *ServeInit, cfg *config.Config, _ none) (doctorReport, error) {
		return doctorReportOf(ctx, cfg), nil
	})
	m["version"] = method(sess, func(context.Context, *ServeInit, *config.Config, none) (Build, error) {
		return versionInfo(), nil
	})
	return m
}
