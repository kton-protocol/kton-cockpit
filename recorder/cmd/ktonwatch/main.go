// ktonwatch reads the event logs libktontrace.so writes and turns them into tasks: which
// command read and wrote which files (with content hashes), which programs it started and
// what network it touched. It executes nothing and publishes nothing; documenting and
// persisting go through the cockpit's verbs.
//
//	ktonwatch -log trace.tsv [-project DIR] [-env a,b] [-json out.json] [-origins exec.jsonl] [-once] [-noise]
//	    follow one log until its END line (or, with -once, until its current end) and print a report
//
//	ktonwatch serve [-dir TRACE_DIR] [-store DIR] [-listen 127.0.0.1:8378] [-idle 15m]
//	    follow every log in TRACE_DIR, keep the observations in DIR, and show them at -listen
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/recorder/ktonmcp"
	"github.com/kton-protocol/kton-cockpit/recorder/watch"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		serve(os.Args[2:])
		return
	}
	single(os.Args[1:])
}

func cacheDir(sub string) string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(d, "kton-cockpit", sub)
}

func classifier(project, env string) *watch.Classifier {
	home, _ := os.UserHomeDir()
	c := &watch.Classifier{Project: abs(project), System: watch.DefaultSystem, Temp: watch.DefaultTemp, Env: watch.DefaultEnv(home)}
	c.AddEnv(strings.Split(env, ",")...)
	return c
}

func abs(p string) string {
	if p == "" {
		return ""
	}
	a, _ := filepath.Abs(p)
	if r, err := filepath.EvalSymlinks(a); err == nil {
		return r
	}
	return a
}

func single(args []string) {
	fs := flag.NewFlagSet("ktonwatch", flag.ExitOnError)
	logPath := fs.String("log", "", "event log written by libktontrace.so")
	project := fs.String("project", "", "project directory (default: from the log header)")
	env := fs.String("env", "", "comma separated prefixes counted as environment (R_HOME, .libPaths, …)")
	jsonOut := fs.String("json", "", "write the tasks as JSON to this file")
	origins := fs.String("origins", "", "Positron executions (positron-exec.jsonl) to attach")
	once := fs.Bool("once", false, "read the log to its current end instead of waiting for END")
	noise := fs.Bool("noise", false, "hide noise in the report (helpers, empty files, loopback)")
	fs.Parse(args)
	if *logPath == "" {
		fmt.Fprintln(os.Stderr, "usage: ktonwatch -log trace.tsv [-project DIR] [-env a,b] [-json out.json] [-origins exec.jsonl] [-once] [-noise]\n       ktonwatch serve [-dir TRACE_DIR] [-store DIR] [-listen 127.0.0.1:8378]")
		os.Exit(2)
	}
	cls := classifier(*project, *env)
	cls.Log = abs(*logPath)
	s := watch.NewSession(strings.TrimSuffix(filepath.Base(*logPath), ".tsv"), cls.Log, cls)

	f, err := os.OpenFile(*logPath, os.O_RDONLY|os.O_CREATE, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r := bufio.NewReader(f)
	partial := ""
	for !s.Closed {
		line, err := r.ReadString('\n')
		partial += line
		if err == io.EOF {
			if *once {
				s.Close("eof", s.Last)
				break
			}
			time.Sleep(20 * time.Millisecond)
			continue
		}
		s.Line(partial)
		partial = ""
	}
	if *origins != "" {
		if o, err := watch.ParseOrigins(*origins); err == nil {
			watch.AttachOrigins(s.Tasks, watch.OriginsIn(o, s.Language))
		}
	}
	watch.LinkVars(s.Tasks)
	watch.Report(os.Stdout, s, watch.Filter{HideNoise: *noise}.View(s))
	if *jsonOut != "" {
		b, _ := json.MarshalIndent(s.Tasks, "", "  ")
		os.WriteFile(*jsonOut, b, 0o644)
	}
}

func serve(args []string) {
	fs := flag.NewFlagSet("ktonwatch serve", flag.ExitOnError)
	dir := fs.String("dir", cacheDir("trace"), "directory the shims write their logs to")
	store := fs.String("store", cacheDir("observations"), "directory the observations are kept in")
	listen := fs.String("listen", "127.0.0.1:8378", "address of the viewer and the read-only API (loopback)")
	idle := fs.Duration("idle", 15*time.Minute, "close a session after this long without data once its processes are gone")
	env := fs.String("env", "", "extra environment prefixes")
	cas := fs.String("cas", cacheDir("cas"), "directory the inputs and outputs of tool calls are kept in, by hash (empty: keep nothing)")
	casMax := fs.Int64("cas-max", 1<<30, "largest file kept in the cas, in bytes")
	mcpOn := fs.Bool("mcp", true, "serve the MCP tools at /mcp for an assistant (bearer token in ~/.config/kton/mcp-token)")
	place := fs.Bool("place", true, "find where a story's sources reside by asking the cockpit (cockpit ask producer/about/scope)")
	web := fs.String("web", defaultWeb(), "the kton-web checkout whose graph viewer draws the flow (default: $KTON_WEB, else what install.sh recorded)")
	fs.Parse(args)

	sv := &watch.Service{
		Dir: *dir, Store: *store, Idle: *idle, Web: *web,
		NewClassifier: func() *watch.Classifier { return classifier("", *env) },
		Logf:          log.Printf,
	}
	if *cas != "" {
		sv.CAS = &watch.CAS{Dir: *cas, MaxSize: *casMax}
	}
	if *place {
		sv.Placer = watch.NewPlacer(watch.CockpitAsk)
	}
	if err := sv.Load(); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		if err := sv.Run(ctx, 500*time.Millisecond); err != nil {
			log.Fatal(err)
		}
	}()
	mux := http.NewServeMux()
	mux.Handle("/kton/reg/", registries(sv))
	if *mcpOn {
		home, _ := os.UserHomeDir()
		tok, err := ktonmcp.Token(filepath.Join(home, ".config", "kton", "mcp-token"))
		if err != nil {
			log.Fatalf("mcp token: %v", err)
		}
		mux.Handle("/mcp", ktonmcp.Handler(ktonmcp.Server(sv, "recorder"), tok))
	}
	mux.Handle("/", sv.Handler())
	srv := &http.Server{Addr: *listen, Handler: mux}
	go func() { <-ctx.Done(); srv.Close() }()
	log.Printf("ktonwatch: logs %s, observations %s, viewer http://%s/", *dir, *store, *listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// defaultWeb: $KTON_WEB, else the checkout recorder/install.sh found next to kton-cockpit and
// wrote to <prefix>/share/kton/web (the prefix is where this binary lives: <prefix>/bin).
func defaultWeb() string {
	if w := os.Getenv("KTON_WEB"); w != "" {
		return w
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(exe)), "share", "kton", "web"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
