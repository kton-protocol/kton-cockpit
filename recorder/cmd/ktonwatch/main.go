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

	"github.com/kton-protocol/kton-cockpit/internal/toolcall"
	"github.com/kton-protocol/kton-cockpit/recorder/ktonmcp"
	v1 "github.com/kton-protocol/kton-cockpit/recorder/v1"
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
	exited := fs.Duration("exited", 3*time.Second, "close a session that owns its log (a terminal command, an Rscript run) this long after its processes exited, so its step can be published at once (0: -idle for all)")
	maxAge := fs.Duration("max-age", 0, "leave out logs and stored sessions without data for longer than this (0: keep all)")
	env := fs.String("env", "", "extra environment prefixes")
	cas := fs.String("cas", cacheDir("cas"), "directory the inputs and outputs of tool calls are kept in, by hash (empty: keep nothing)")
	casMax := fs.Int64("cas-max", 1<<30, "largest file kept in the cas, in bytes")
	mcpOn := fs.Bool("mcp", true, "serve the MCP tools at /mcp for an assistant (bearer token in ~/.config/kton/mcp-token)")
	place := fs.Bool("place", true, "find where a story's sources reside by asking the cockpit (cockpit ask producer/about/scope)")
	web := fs.String("web", defaultWeb(), "the kton-web checkout whose graph viewer draws the flow (default: $KTON_WEB, else what install.sh recorded)")
	fs.Parse(args)

	sv := &watch.Service{
		Dir: *dir, Store: *store, Idle: *idle, Exited: *exited, MaxAge: *maxAge, OldEvery: 30 * time.Second, Web: *web,
		NewClassifier: func() *watch.Classifier { return classifier("", *env) },
		Logf:          log.Printf,
	}
	if *cas != "" {
		sv.CAS = &toolcall.CAS{Dir: *cas, MaxSize: *casMax}
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
	home, _ := os.UserHomeDir()
	tok, err := ktonmcp.Token(filepath.Join(home, ".config", "kton", "mcp-token"))
	if err != nil {
		log.Fatalf("token: %v", err)
	}
	if *mcpOn {
		mux.Handle("/mcp", ktonmcp.Handler(ktonmcp.Server(sv, "recorder"), tok))
	}
	api := &v1.Server{Token: tok} // the service API on the cockpit's held union, same token
	defer api.Close()
	mux.Handle("/v1/", api.Handler())
	mux.Handle("/", sv.Handler())
	srv := &http.Server{Addr: *listen, Handler: mux}
	go func() { <-ctx.Done(); srv.Close() }()
	// a new ktonwatch installed over this one: start it in this process (MH 12.11.: after an install the
	// old server kept running and answered 'unknown action' for what the new extension asked)
	replaced := make(chan string, 1)
	go watchOwnBinary(replaced, func() { srv.Close() })
	log.Printf("ktonwatch: logs %s, observations %s, viewer http://%s/", *dir, *store, *listen)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	select {
	case exe := <-replaced:
		log.Printf("ktonwatch: %s was replaced; starting the new one with the same arguments", exe)
		if err := restartAs(exe); err != nil {
			log.Fatalf("ktonwatch: could not start the new binary: %v", err)
		}
	default:
	}
}

// watchOwnBinary: the executable this process was started from, looked at every 15 s; when another
// file stands there (an install puts a new one in place by rename), and it has not changed for a few
// seconds, stop serving and hand its path on.
func watchOwnBinary(replaced chan<- string, stop func()) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	first, err := os.Stat(exe)
	if err != nil {
		return
	}
	for range time.Tick(15 * time.Second) {
		now, err := os.Stat(exe)
		if err != nil || os.SameFile(first, now) || now.Mode()&0o111 == 0 {
			continue
		}
		time.Sleep(3 * time.Second) // let the install finish writing
		if again, err := os.Stat(exe); err != nil || again.Size() != now.Size() || !again.ModTime().Equal(now.ModTime()) {
			continue
		}
		replaced <- exe
		stop()
		return
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
