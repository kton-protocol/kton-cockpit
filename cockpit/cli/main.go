// Package cli is the cockpit command line: the verbs (publish, say, ask), run, install, workflow,
// scope, show, init, doctor, keygen and the MCP surface. It holds no kernel logic of its own — the
// kernels are linked as libraries through the cockpit package.
//
// It is a package rather than a main so that a binary composed with an extension backend
// (cockpit-improve, ADR-008) is this same program plus one import.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/cockpit/backend"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/container"
	"github.com/kton-protocol/kton-cockpit/internal/material"
	"github.com/kton-protocol/kton-cockpit/internal/mcpsurface"
	"github.com/kton-protocol/kton-cockpit/internal/show"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Main is the cockpit command line. A composed binary (cockpit-improve: the core plus an extension
// backend, ADR-008) calls it from its own main after importing its backends.
func Main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx := context.Background()
	var err error
	switch os.Args[1] {
	case "mcp":
		err = runMCP(ctx)
	case "init":
		err = runInit(ctx)
	case "show":
		err = runShow(ctx, os.Args[2:])
	case "doctor":
		err = runDoctor(ctx, os.Args[2:])
	case "scope":
		err = runScope(ctx, os.Args[2:])
	case "publish", "say", "ask":
		err = runVerb(ctx, os.Args[1], os.Args[2:])
	case "version":
		err = runVersion()
	case "run":
		err = runRun(ctx, os.Args[2:])
	case "keygen":
		err = runKeygen(ctx, os.Args[2:])
	case "pin":
		err = runPin(ctx, os.Args[2:])
	case "trust":
		err = runTrust(ctx, os.Args[2:])
	case "federation":
		err = runFederation(ctx, os.Args[2:])
	case "sync":
		err = runSync(ctx, os.Args[2:])
	case "config":
		err = runConfig(ctx, os.Args[2:])
	case "index":
		err = runIndex(ctx, os.Args[2:])
	case "install":
		err = runInstall(ctx, os.Args[2:])
	case "workflow":
		err = runWorkflow(ctx, os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if errors.Is(err, errNotReady) {
		// Doctor ran and said what is missing; the exit code says it to a script.
		os.Exit(exitNotReady)
	}
	if err != nil {
		// Configuration errors carry the prefix themselves, for the MCP surface; once is enough.
		fmt.Fprintln(os.Stderr, "cockpit:", strings.TrimPrefix(err.Error(), "cockpit: "))
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `cockpit - kton-cockpit

usage:
  cockpit publish --in FILE... --out FILE... -- COMMAND   record a result as a signed foton
  cockpit say     <template> <run|file|sha256:id> [field=value]...   claim something about it
  cockpit ask     <question> <run|file|sha256:id>          ask, re-verified against your trust tiers
                  say and ask with no arguments list the templates and the questions; --json prints
                  the full answer. A JSON argument ('{...}' [--field NAME]) works as before.

  cockpit run --dir DIR --in [NAME=]FILE... -- COMMAND...
                   run one step in DIR (inputs copied in), record what it wrote there
%s  cockpit run      also run folders: new / <slug> / list — clone inputs, execute, record
  cockpit scope    seed / seal / read a nekton scope
  cockpit pin      pin the image publish and run execute in, by digest (pin <image>)
  cockpit trust    add <tier> <peer checkout|git URL|source name> / list / remove <tier>
  cockpit federation  add <name> <git-URL|checkout> / list / pull / remove — read other registries
  cockpit sync     take in what others shared, share what is here, then pull the federation sources
  cockpit config   show the configuration, or set one value (config persist.share off)
  cockpit index    status / start / stop / rebuild — the machine's index daemon (index.kind oxigraph)
  cockpit keygen   make a signing identity (keys/<name>.key + .pub, and the claims pair)
  cockpit version  what this binary is, and which kernel is compiled into it
  cockpit mcp      start the MCP stdio server (cockpit_publish/cockpit_say/cockpit_ask)
  cockpit init     scaffold cockpit.config.json in the current git repo
  cockpit doctor   validate cockpit.config.json + the repo binding; --json for a program,
                   exit 3 while a step marked ! is left
  cockpit show     serve this repo's records to a kton-web viewer
                   [addr] [--web <kton-web checkout>|$KTON_WEB]

a session never invokes 'init'/'doctor'/'show' — they are for the human operator setting up or
inspecting a participant repo. Only 'mcp' is registered as the tool surface (via .mcp.json), and
it exposes exactly three verbs.
`, packageUsage)
}

func runMCP(ctx context.Context) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "kton-cockpit", Version: Version}, nil)
	c := cockpit.New(cockpit.Start{})

	mcp.AddTool(server, &mcp.Tool{
		Name: "cockpit_publish",
		Description: "veröffentlichen: register a work result as a signed foton. Commits the given " +
			"input/output paths, builds commit-pinned permalinks, and records the computation via " +
			"`plankton author`. This is the only way to make git commits or plankton records in this repo.",
	}, mcpsurface.Handler(c.Publish))

	mcp.AddTool(server, &mcp.Tool{
		Name: "cockpit_say",
		Description: "sagen: bind a claim, from an allowed template, to a foton or file via " +
			"`nekton annotate`. For the reproduces template, the cockpit itself determines the " +
			"achieved level by running the reproduction precondition — it is never self-declared.",
	}, mcpsurface.Handler(c.Say))

	mcp.AddTool(server, &mcp.Tool{
		Name: "cockpit_ask",
		Description: "fragen: query the registry graph (producer/uses/lineage/reproductions/about/by). " +
			"Every returned record is independently re-verified against this repo's configured trust " +
			"tiers before being included — never trusted from its declared keyid. " +
			"query sparql takes a SELECT or ASK of your own (field sparql) over the kernel's RDF projection: " +
			"steps (prov:Activity, prov:used, prov:wasGeneratedBy) in the default graph, attributed with " +
			"prov:wasAttributedTo only where a trusted key verified them; claims a trusted key signed as " +
			"nanopublications in named graphs (GRAPH ?a { ?subject ?predicate ?o }). " +
			"A file or record sha256:<hex> is <https://kton.dev/o/<hex>>. No FROM, no SERVICE.",
	}, mcpsurface.Handler(c.Ask))

	return server.Run(ctx, &mcp.StdioTransport{})
}

func runInit(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Every backend is asked how it would bind this folder; one that recognises it (git: a working
	// tree) wins over the fallback that binds any folder to itself (local, ADR-004).
	prop, ok := backend.Propose(ctx, cwd)
	if !ok {
		return fmt.Errorf("no backend in this build can bind %s", cwd)
	}
	root := prop.Root
	cfgPath := filepath.Join(root, "cockpit.config.json")
	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("%s already exists — remove it first if you want to regenerate", cfgPath)
	}
	var repo config.RepoRef
	if err := json.Unmarshal(prop.Block, &repo); err != nil {
		return err
	}
	raw := config.Raw{
		Repo: repo,
		Paths: config.Paths{
			PlanktonDir:  "registry/plankton",
			NektonDir:    "registry/nekton",
			TemplatesDir: "templates",
			KeysDir:      "keys",
			BinDir:       "bin",
		},
		Identity: config.Identity{
			SessionID:   "session-1",
			PlanktonKey: "keys/session-1.key",
			NektonKey:   "keys/session-1-claims.key",
		},
		Verbs:  config.Verbs{Publish: true, Say: true, Ask: true},
		Claims: config.Claims{AllowedTemplates: []string{"describes", "rationale", "reproduces", "working-on"}},
		Trust: config.Trust{
			Tiers: map[string][]string{
				"self": {},
			},
		},
		Reproduction: config.Reproduction{RequiredLevel: "L0"},
	}

	notes, err := scaffold(root, &raw)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		return err
	}
	todo := "" // what is left is said in the notes below, and `doctor` lists it
	fmt.Printf("wrote %s (%s%s)\n", cfgPath, prop.Describe, todo)
	for _, n := range notes {
		fmt.Println("  " + n)
	}
	return nil
}

// runShow serves this repo's records to a kton-web viewer. It is an operator subcommand alongside
// init and doctor — NOT a fourth verb: the MCP surface is unchanged at three, and nothing here
// is reachable from it.
func runShow(ctx context.Context, args []string) error {
	// Loopback, not every interface. ":8377" would bind all of them while the URL printed below says
	// localhost — so a repo's whole graph, including a private one's, would be reachable from the
	// network to anyone who could reach the host, and nothing on screen would suggest it. Pass an
	// explicit host to widen it deliberately.
	addr := "127.0.0.1:8377" // the port kton-web's own serve.sh uses, for the WSL reason it documents
	webDir := os.Getenv("KTON_WEB")
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--web" && i+1 < len(args):
			i++
			webDir = args[i]
		case strings.HasPrefix(args[i], "-"):
			return fmt.Errorf("usage: cockpit show [addr] [--web <kton-web checkout>]")
		default:
			addr = args[i]
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(ctx, cwd)
	if err != nil {
		return err
	}

	handler, err := show.New(cfg).Handler(webDir)
	if err != nil {
		return err
	}

	if strings.HasPrefix(addr, ":") {
		fmt.Printf("note: %s binds every interface, not just loopback — this repo's records will be\n", addr)
		fmt.Printf("      reachable from the network by anyone who can reach this host.\n\n")
	}
	base := "http://" + displayAddr(addr)
	fmt.Printf("cockpit show on %s/  (registry %s)\n", base, cfg.RepoRoot)
	fmt.Printf("  data     %s/data/union.json  keys.json  names.json\n", base)
	if webDir == "" {
		fmt.Printf("\nNo kton-web checkout given (--web or KTON_WEB), so only the data is served. Point any\n")
		fmt.Printf("viewer at it: <viewer>/?union=%s/data/union.json\n", base)
	} else {
		fmt.Printf("  graph    %s/viewers/graph/?union=/data/union.json\n", base)
	}
	fmt.Printf("\nkeys.json holds this repo's CONFIGURED trust tiers, so the viewer re-verifies against\n")
	fmt.Printf("exactly what cockpit_ask does — not every pubkey that happens to sit in the registry.\n")
	return http.ListenAndServe(addr, handler)
}

// displayAddr turns a listen address into something clickable: ":8377" is a valid thing to listen
// on but not to open.
func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}

func runDoctor(ctx context.Context, args []string) error {
	asJSON := false
	for _, a := range args {
		if a != "--json" {
			return fmt.Errorf("doctor takes no argument but --json, not %q", a)
		}
		asJSON = true
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(ctx, cwd)
	if err != nil {
		return err
	}
	if asJSON {
		return printDoctorJSON(ctx, cfg)
	}

	fmt.Printf("repo root:      %s\n", cfg.RepoRoot)
	if cfg.Raw.Repo.IsLocal() {
		fmt.Printf("bound to:       %s (local mode — verified that this config is where it says it is)\n", cfg.Raw.Repo.Root)
	} else {
		fmt.Printf("bound to:       %s/%s (verified against origin remote)\n", cfg.Raw.Repo.Owner, cfg.Raw.Repo.Name)
	}
	fmt.Printf("plankton_dir:   %s\n", checkMadePath(cfg.PlanktonDir))
	fmt.Printf("nekton_dir:     %s\n", checkMadePath(cfg.NektonDir))
	fmt.Printf("templates_dir:  %s\n", checkPath(cfg.TemplatesDir))
	fmt.Printf("plankton key:   %s\n", checkKeyPath(cfg.PlanktonKey))
	fmt.Printf("nekton key:     %s\n", checkKeyPath(cfg.NektonKey))
	fmt.Printf("allowed templates: %v\n", cfg.Raw.Claims.AllowedTemplates)
	// Reported with the live chain rather than as the configured id: the id being well-formed says
	// nothing about the shape a claim would join. Nothing here stops writing — completeness is
	// judged when a seal is relied upon, not when a claim is made — but an operator should see a
	// branch or a gap before someone relies on it.
	if scopes := cfg.Raw.Claims.Scopes; len(scopes) > 0 {
		names := make([]string, 0, len(scopes))
		for n := range scopes {
			names = append(names, n)
		}
		sort.Strings(names)
		run := binaries.New(cfg)
		for i, name := range names {
			label := "claim scopes:"
			if i > 0 {
				label = ""
			}
			id := scopes[name]
			head, err := run.Head(ctx, id)
			if err != nil {
				fmt.Printf("%-15s %-12s %s  [UNREADABLE: %v]\n", label, name, id, err)
				continue
			}
			fmt.Printf("%-15s %-12s %s\n", label, name, id)
			fmt.Printf("                %-12s tip %s  (%d chained)\n", "", head.Heads[0], head.ChainLength)
			if head.Branched {
				fmt.Printf("                %-12s BRANCHED into %d heads: each seals only its own branch, and a\n"+
					"                %-12s claim carries one prev, so nothing rejoins them. `cockpit_ask`\n"+
					"                %-12s query \"scope\" reports which.\n", "", len(head.Heads), "", "")
			}
			if head.Unresolved > 0 {
				fmt.Printf("                %-12s %d claim(s) name it with a predecessor this registry does not\n"+
					"                %-12s hold. The view is partial, not broken: another source may have it.\n",
					"", head.Unresolved, "")
			}
		}
	} else {
		fmt.Printf("claim scopes:   none configured — every claim stands on its own\n")
	}
	fmt.Printf("trust tiers:    %v\n", cfg.Raw.Trust.Tiers)

	// Last, and read out of this binary rather than off disk: the kernel is linked in, so which
	// build will run is not a question an operator can answer wrongly and not one a check can
	// catch late. What it reports is the module version Go recorded when this cockpit was built.
	fmt.Printf("kernel:         linked, not invoked — no plankton/nekton process is ever started\n")
	for _, mod := range linkedKernels() {
		where := mod.Version
		if mod.Replace != nil {
			// A `replace` is what is actually compiled in, so it is what gets reported. Saying
			// v0.0.0 while the bytes came from a directory on this machine would be the version of
			// this problem one level up.
			where = mod.Replace.Path
			if mod.Replace.Version != "" {
				where += " " + mod.Replace.Version
			}
			where = "replaced by " + where
		}
		fmt.Printf("                %-20s %s\n", mod.Path, where)
	}

	mode := cfg.Raw.Repo.ModeOrDefault()
	switch {
	case cfg.Raw.Repo.IsLocal():
		fmt.Printf("persist:        none (%s) — nothing is made durable or shared, and fotons carry no\n", mode)
		fmt.Printf("                locators (a locator needs a revision to pin)\n")
	case !cfg.Raw.CommitEnabled():
		fmt.Printf("persist:        OFF (%s) — records are signed and registered, files are not made durable,\n", mode)
		fmt.Printf("                and fotons carry no locators (no revision exists for one to pin)\n")
	case !cfg.Raw.ShareEnabled():
		fmt.Printf("persist:        commit on, share OFF (%s) — locators are built and correct, but do not\n", mode)
		fmt.Printf("                resolve until someone shares\n")
	default:
		fmt.Printf("persist:        commit + share (%s)\n", mode)
	}

	if cfg.Raw.Anchor.Enabled {
		where := "the public Sigstore Rekor log"
		if cfg.Raw.Anchor.RekorURL != "" {
			where = cfg.Raw.Anchor.RekorURL + " (key pinned)"
		}
		fmt.Printf("anchor:         every record is witnessed in %s\n", where)
		fmt.Printf("                the WHOLE envelope is submitted, so the command, every input/output path\n")
		fmt.Printf("                and hash, and the locators (which name %s/%s) become public\n",
			cfg.Raw.Repo.Owner, cfg.Raw.Repo.Name)
		fmt.Printf("                and stay public. File contents do not — only their hashes.\n")
	} else {
		fmt.Printf("anchor:         off — records are signed, but nothing independent attests WHEN they existed\n")
	}

	// Reported before anything is published, because the failure it names is otherwise silent: a
	// certificate for the wrong key, or one with no configured root to judge it against, produces
	// valid records that report CARRIED forever while the operator believes an identity was
	// established. See material.Preflight.
	if reports := material.Preflight(cfg); len(reports) > 0 {
		for i, m := range reports {
			label := "material:"
			if i > 0 {
				label = "         "
			}
			// The reason is printed beside the verdict, not buried in the detail line: for a
			// failure it is the actionable half. `not-bound` is a line in this config to fix,
			// `unreadable` is damaged or tampered bytes to investigate, `chain` is a certificate
			// to renew or a root to add. Those are three different afternoons.
			verdict := string(m.Verdict)
			if m.Reason != "" {
				verdict += " (" + string(m.Reason) + ")"
			}
			fmt.Printf("%-15s %s → %s\n", label, m.Scheme, verdict)
			if m.Detail != "" {
				fmt.Printf("                %s\n", m.Detail)
			}
		}
	} else {
		fmt.Printf("material:       none configured — records carry the signature and nothing about whose key it is\n")
	}

	if cfg.Raw.Union.Publish {
		fmt.Printf("union:          published to %s on every record (viewable online without `cockpit show`)\n",
			cfg.Raw.Union.DirOrDefault())
	} else {
		fmt.Printf("union:          not published — the graph is reachable via `cockpit show` only\n")
	}

	if !cfg.Raw.Execution.Enabled() {
		fmt.Printf("execution:      not configured — cockpit_publish RECORDS the command, does not run it\n")
		if cfg.Raw.Environment.EnvRef != "" {
			fmt.Printf("                (environment.envRef is set, so the pinned environment is asserted, not observed)\n")
		}
		return printReadiness(ctx, cfg)
	}
	fmt.Printf("execution:      %s in %s\n", cfg.Raw.Execution.EngineOrDefault(), cfg.Raw.Execution.Image)
	ver, verr := container.Version(ctx, cfg)
	if verr != nil {
		_ = printReadiness(ctx, cfg) // still said: the steps are as true as before; the engine error wins
		return fmt.Errorf("execution is configured but the engine is unusable: %w", verr)
	}
	fmt.Printf("engine ver:     %s\n", ver)
	if cfg.Raw.Execution.Network {
		fmt.Printf("network:        ALLOWED — runs are not isolated, so their environment claim is weaker\n")
	} else {
		fmt.Printf("network:        isolated (--network none)\n")
	}
	return printReadiness(ctx, cfg)
}

func checkPath(p string) string {
	if pathState(p) != stateOK {
		return p + "  [MISSING]"
	}
	return p + "  [ok]"
}

// checkKeyPath is checkPath plus the one thing a private signing key has that an ordinary path does
// not: a file mode that is supposed to keep other users out.
//
// A mode is a request the platform may decline. On a Windows drive mounted into WSL — which is
// where this repository itself lives — a 0600 request lands as 0777, and every statement this
// project makes about a private key being unreadable by others is false there. kton's `keygen`
// warns at creation time, but that warning goes to stderr of a command the operator runs once and
// the cockpit does not run at all; a key copied in from elsewhere, or a repo moved onto such a
// mount afterwards, is never announced by anyone.
//
// So it is checked here, where "is this repository set up correctly" is the question, and it is
// checked every time rather than once.
// keyModeExposed answers the one question, separately from any filesystem, so it can be checked
// exhaustively: does this mode let anyone but the owner read the key? Group and other, read write
// or execute — a key a stranger can write is no better than one they can read.
func keyModeExposed(perm os.FileMode) bool { return perm&0o077 != 0 }

func checkKeyPath(p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		return p + "  [MISSING]"
	}
	if perm := fi.Mode().Perm(); keyModeExposed(perm) {
		return fmt.Sprintf("%s  [READABLE BY OTHERS: mode %v]\n"+
			"                this is a PRIVATE signing key. Either the filesystem does not enforce\n"+
			"                modes (a Windows drive mounted into WSL, FAT/exFAT, some network mounts)\n"+
			"                or the mode was widened. Anyone who can read it can sign as this repo.", p, perm)
	}
	return p + "  [ok]"
}

// linkedKernels reports the kton modules compiled into this binary, in the order they are listed
// above. Go records a dependency's version in the build info, so "which kernel is this" has an
// answer that does not depend on anyone keeping a file in `bin/` in step with a checkout — the
// failure this replaced. A binary built with `go build` from a module has these; one built in a
// way that strips build info has none, and then the list is simply empty rather than wrong.
func linkedKernels() []*debug.Module {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	var out []*debug.Module
	for _, dep := range info.Deps {
		if strings.HasPrefix(dep.Path, "kton.dev/") {
			out = append(out, dep)
		}
	}
	return out
}

// --- the three verbs, at a command line ---
//
// The same three cockpit methods the MCP server registers above, reached a second way. That is the
// whole of it: no CLI-only path through publish, no flag that loosens a guard, no answer a session
// would not also get. `TestVerbs_TheGuardsApplyAtTheCommandLineToo` holds the guards to that.
//
// They exist because this is a command-line tool and its three verbs could not be typed. The
// examples stood in for them with an MCP client written in Python, so every example demonstrated
// its own harness — "to publish a result, run run.sh" — which teaches nobody anything and hid the
// gap for as long as it was the only way anyone drove the binary.
//
// Reaching them from a shell grants nothing that was withheld: whoever can type this already has
// the registry, the keys and the config on their filesystem. What the tool surface withholds from a
// SESSION it still withholds, because the ceiling is the configuration, not the transport.

// callVerb decodes one verb's arguments, runs the handler, and prints what came back.
//
// Unknown fields are refused rather than ignored. Over MCP the schema catches a misspelled
// argument; at a shell nothing would, and `{"output":["x"]}` for `outputs` would publish a record
// naming no outputs at all — signed, valid, and wrong, which is the failure this repository exists
// to prevent one level down.
func callVerb[In, Out any](ctx context.Context, raw, field string,
	fn func(context.Context, In) (*Out, error)) error {
	var in In
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("the argument is not valid JSON for this verb: %w", err)
	}
	out, err := fn(ctx, in)
	exitIfRefused(err)
	if err != nil {
		return err
	}
	return printResult(out, field)
}

// exitIfRefused ends the process with exit 2 when err is the cockpit declining. A refusal is not a
// crash: it is the cockpit saying no, with a reason, and it leaves by a different exit code so a
// script can tell "you may not" from "something broke".
func exitIfRefused(err error) {
	var r *cockpit.Refusal
	if errors.As(err, &r) {
		fmt.Fprintln(os.Stderr, r.Reason)
		os.Exit(2)
	}
}

// printResult prints the whole answer, or one field of it. `--field` exists because the answers are
// objects and a shell wants one value out of them; without it every caller grows its own JSON
// parser, which is how an id comes to be read off a line of prose by position.
func printResult(out any, field string) error {
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if field == "" {
		fmt.Println(string(b))
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	v, ok := m[field]
	if !ok {
		// Said rather than printed as empty: a field that is not there and a field that is there
		// and empty are different answers, and a script that cannot tell them apart will read the
		// first as the second.
		return fmt.Errorf("this answer has no field %q; it has %s", field, strings.Join(sortedFields(m), ", "))
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		fmt.Println(s)
		return nil
	}
	// Compacted, not re-printed from the indented form: a shell consumer wants one line, and the
	// inner indentation of a nested value carries the outer document's margins with it.
	var buf bytes.Buffer
	if err := json.Compact(&buf, v); err != nil {
		return err
	}
	fmt.Println(buf.String())
	return nil
}

func sortedFields(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// verbArgs splits `<json> [--field NAME]` into its two parts.
func verbArgs(verb string, args []string) (raw, field string, err error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--field" {
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("--field needs the name of a field")
			}
			field = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) != 1 {
		return "", "", fmt.Errorf("usage: cockpit %s '<json arguments>' [--field NAME]", verb)
	}
	return rest[0], field, nil
}

func runVerb(ctx context.Context, verb string, args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		switch verb {
		case "publish":
			return verbHelp[cockpit.PublishRequest](os.Stdout, verb)
		case "say":
			return verbHelp[cockpit.SayRequest](os.Stdout, verb)
		case "ask":
			return verbHelp[cockpit.AskRequest](os.Stdout, verb)
		}
	}
	// A JSON argument is the form scripts and the MCP surface share, unchanged. Anything else is
	// the same verb typed at a shell (shellask.go, shellsay.go, shellpublish.go).
	if len(args) == 0 || !strings.HasPrefix(strings.TrimSpace(args[0]), "{") {
		switch verb {
		case "publish":
			return shellPublish(ctx, args)
		case "say":
			return shellSay(ctx, args)
		case "ask":
			return shellAsk(ctx, args)
		}
	}
	raw, field, err := verbArgs(verb, args)
	if err != nil {
		return err
	}
	c := cockpit.New(cockpit.Start{})
	switch verb {
	case "publish":
		return callVerb(ctx, raw, field, c.Publish)
	case "say":
		return callVerb(ctx, raw, field, c.Say)
	case "ask":
		return callVerb(ctx, raw, field, c.Ask)
	}
	return fmt.Errorf("no verb %q", verb)
}

// Version is this cockpit's own version, set at build time with
// `-ldflags "-X github.com/kton-protocol/kton-cockpit/cockpit/cli.Version=v0.2.0"`. A binary built without it says so rather than claiming a
// number nobody stamped: "which build is this" is a question a release has to be able to answer,
// and an invented answer is worse than none.
var Version = "(devel)"

// runVersion reports this binary and the kernel inside it.
//
// Both, always, and on one screen: the cockpit's own version says nothing about which kton wrote a
// store, and which kton wrote a store is what decides whether that store reads as populated or as
// empty-with-exit-0. An operator comparing two machines needs the pair.
func runVersion() error {
	fmt.Printf("cockpit %s\n", Version)
	if info, ok := debug.ReadBuildInfo(); ok {
		if rev := buildSetting(info, "vcs.revision"); rev != "" {
			dirty := ""
			if buildSetting(info, "vcs.modified") == "true" {
				dirty = " (uncommitted changes)"
			}
			fmt.Printf("  built from %s%s\n", rev, dirty)
		} else if v := info.Main.Version; v != "" && v != "(devel)" {
			// `go install …@main` stamps no vcs settings; its module version is a pseudo-version,
			// which ends in the commit.
			fmt.Printf("  module %s\n", v)
		}
		fmt.Printf("  %s\n", info.GoVersion)
	}
	for _, mod := range linkedKernels() {
		where := mod.Version
		if mod.Replace != nil {
			where = "replaced by " + mod.Replace.Path
			if mod.Replace.Version != "" {
				where += " " + mod.Replace.Version
			}
		}
		fmt.Printf("  %-20s %s\n", mod.Path, where)
	}
	return nil
}

func buildSetting(info *debug.BuildInfo, key string) string {
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}
