// Command cockpit is the Claude-Science-Cockpit: it exposes exactly three verbs to Claude
// (publish/say/ask) over MCP, and reimplements no plankton/nekton kernel logic — every mutation
// and query shells out to the vendored bin/plankton, bin/nekton binaries.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/container"
	"github.com/deathbychoco/claude-science-cockpit/internal/show"
	"github.com/deathbychoco/claude-science-cockpit/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
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
		err = runDoctor(ctx)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cockpit:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `cockpit - Claude-Science-Cockpit

usage:
  cockpit mcp      start the MCP stdio server (cockpit_publish/cockpit_say/cockpit_ask)
  cockpit init     scaffold cockpit.config.json in the current git repo
  cockpit doctor   validate cockpit.config.json + the repo binding
  cockpit show     serve this repo's records to a kton-web viewer
                   [addr] [--web <kton-web checkout>|$KTON_WEB]

Claude never invokes 'init'/'doctor'/'show' — they are for the human operator setting up or
inspecting a participant repo. Only 'mcp' is registered as the tool surface (via .mcp.json), and
it exposes exactly three verbs.
`)
}

func runMCP(ctx context.Context) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "claude-science-cockpit", Version: "0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "cockpit_publish",
		Description: "veröffentlichen: register a work result as a signed foton. Commits the given " +
			"input/output paths, builds commit-pinned permalinks, and records the computation via " +
			"`plankton author`. This is the only way to make git commits or plankton records in this repo.",
	}, tools.Publish)

	mcp.AddTool(server, &mcp.Tool{
		Name: "cockpit_say",
		Description: "sagen: bind a claim, from an allowed template, to a foton or file via " +
			"`nekton annotate`. For the reproduces template, the cockpit itself determines the " +
			"achieved level by running the reproduction precondition — it is never self-declared.",
	}, tools.Say)

	mcp.AddTool(server, &mcp.Tool{
		Name: "cockpit_ask",
		Description: "fragen: query the registry graph (producer/uses/lineage/reproductions/about/by). " +
			"Every returned record is independently re-verified against this repo's configured trust " +
			"tiers before being included — never trusted from its declared keyid.",
	}, tools.Ask)

	return server.Run(ctx, &mcp.StdioTransport{})
}

func runInit(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Outside a git repository, scaffold a local-mode config rather than refusing: running without
	// git is a supported mode, and the current directory is what it binds to. See ADR-004.
	root, err := gitRoot(ctx, cwd)
	local := err != nil
	if local {
		root = cwd
	}
	cfgPath := filepath.Join(root, "cockpit.config.json")
	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("%s already exists — remove it first if you want to regenerate", cfgPath)
	}

	owner, name := "", ""
	if url, err := gitOriginURL(ctx, root); err == nil {
		owner, name = parseOwnerRepo(url)
	}

	repo := config.RepoRef{Owner: owner, Name: name}
	if local {
		repo = config.RepoRef{Mode: config.ModeLocal, Root: root}
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
		Claims: config.Claims{AllowedTemplates: []string{"reproduces", "working-on"}},
		Trust: config.Trust{
			Tiers: map[string][]string{
				"self": {},
			},
		},
		Reproduction: config.Reproduction{RequiredLevel: "L0"},
	}

	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		return err
	}
	if local {
		fmt.Printf("wrote %s (local mode, bound to %s — no git repository here; fill in trust.tiers before use)\n", cfgPath, root)
	} else {
		fmt.Printf("wrote %s (owner=%q name=%q — fill in trust.tiers before use)\n", cfgPath, owner, name)
	}
	return nil
}

// runShow serves this repo's records to a kton-web viewer. It is an operator subcommand alongside
// init and doctor — NOT a fourth verb: Claude's MCP surface is unchanged at three, and nothing here
// is reachable from it.
func runShow(ctx context.Context, args []string) error {
	addr := ":8377" // the port kton-web's own serve.sh uses, for the same WSL reason it documents
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

func runDoctor(ctx context.Context) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.Load(ctx, cwd)
	if err != nil {
		return err
	}

	fmt.Printf("repo root:      %s\n", cfg.RepoRoot)
	if cfg.Raw.Repo.IsLocal() {
		fmt.Printf("bound to:       %s (local mode — verified that this config is where it says it is)\n", cfg.Raw.Repo.Root)
	} else {
		fmt.Printf("bound to:       %s/%s (verified against origin remote)\n", cfg.Raw.Repo.Owner, cfg.Raw.Repo.Name)
	}
	fmt.Printf("plankton_dir:   %s\n", checkPath(cfg.PlanktonDir))
	fmt.Printf("nekton_dir:     %s\n", checkPath(cfg.NektonDir))
	fmt.Printf("templates_dir:  %s\n", checkPath(cfg.TemplatesDir))
	fmt.Printf("plankton bin:   %s\n", checkPath(filepath.Join(cfg.BinDir, "plankton")))
	fmt.Printf("nekton bin:     %s\n", checkPath(filepath.Join(cfg.BinDir, "nekton")))
	fmt.Printf("plankton key:   %s\n", checkPath(cfg.PlanktonKey))
	fmt.Printf("nekton key:     %s\n", checkPath(cfg.NektonKey))
	fmt.Printf("allowed templates: %v\n", cfg.Raw.Claims.AllowedTemplates)
	fmt.Printf("trust tiers:    %v\n", cfg.Raw.Trust.Tiers)

	// Last, and reported by the binaries themselves rather than from anything recorded: which
	// kernel build will actually run decides whether a store reads as populated or as empty with
	// exit 0, and whether the flags the cockpit depends on exist at all.
	r := binaries.New(cfg)
	plankton, nekton, verr := r.KernelVersions(ctx)
	if verr != nil {
		return fmt.Errorf("kernel: %w", verr)
	}
	fmt.Printf("plankton ver:   %s\n", plankton)
	fmt.Printf("nekton ver:     %s\n", nekton)
	if err := r.CheckKernel(ctx); err != nil {
		return fmt.Errorf("kernel too old: %w", err)
	}
	fmt.Printf("kernel:         meets the required %d.%d minimum  [ok]\n",
		binaries.RequiredKernelMajor, binaries.RequiredKernelMinor)

	switch {
	case cfg.Raw.Repo.IsLocal():
		fmt.Printf("git:            none — no repository, so no commits, no pushes, and fotons carry no\n")
		fmt.Printf("                locators (a permalink needs an owner/repo and a commit)\n")
	case !cfg.Raw.CommitEnabled():
		fmt.Printf("git:            commits OFF — records are signed and registered, files are not committed,\n")
		fmt.Printf("                and fotons carry no locators (no commit exists for a permalink to pin)\n")
	case !cfg.Raw.PushEnabled():
		fmt.Printf("git:            commits on, push OFF — permalinks are built and correct, but do not\n")
		fmt.Printf("                resolve until someone pushes\n")
	default:
		fmt.Printf("git:            commit + push\n")
	}

	if cfg.Raw.Anchor.Enabled {
		where := "the public Sigstore Rekor log"
		if cfg.Raw.Anchor.RekorURL != "" {
			where = cfg.Raw.Anchor.RekorURL + " (key pinned)"
		}
		fmt.Printf("anchor:         every record is witnessed in %s\n", where)
		fmt.Printf("                the WHOLE envelope is submitted, so the command, every input/output path\n")
		fmt.Printf("                and hash, and the permalinks (which name %s/%s) become public\n",
			cfg.Raw.Repo.Owner, cfg.Raw.Repo.Name)
		fmt.Printf("                and stay public. File contents do not — only their hashes.\n")
	} else {
		fmt.Printf("anchor:         off — records are signed, but nothing independent attests WHEN they existed\n")
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
		return nil
	}
	fmt.Printf("execution:      %s in %s\n", cfg.Raw.Execution.EngineOrDefault(), cfg.Raw.Execution.Image)
	ver, verr := container.Version(ctx, cfg)
	if verr != nil {
		return fmt.Errorf("execution is configured but the engine is unusable: %w", verr)
	}
	fmt.Printf("engine ver:     %s\n", ver)
	if cfg.Raw.Execution.Network {
		fmt.Printf("network:        ALLOWED — runs are not isolated, so their environment claim is weaker\n")
	} else {
		fmt.Printf("network:        isolated (--network none)\n")
	}
	return nil
}

func checkPath(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p + "  [MISSING]"
	}
	return p + "  [ok]"
}

func gitRoot(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitOriginURL(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func parseOwnerRepo(url string) (owner, name string) {
	url = strings.TrimSuffix(url, ".git")
	parts := strings.FieldsFunc(url, func(r rune) bool { return r == '/' || r == ':' })
	if len(parts) < 2 {
		return "", ""
	}
	return parts[len(parts)-2], parts[len(parts)-1]
}
