// Command cockpit is the Claude-Science-Cockpit: it exposes exactly three verbs to Claude
// (publish/say/ask) over MCP, and reimplements no plankton/nekton kernel logic — every mutation
// and query shells out to the vendored bin/plankton, bin/nekton binaries.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/container"
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
  cockpit doctor   validate cockpit.config.json + the repo/remote binding

Claude never invokes 'init'/'doctor' — they are for the human operator setting up a participant
repo. Only 'mcp' is registered as the tool surface (via .mcp.json).
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
	root, err := gitRoot(ctx, cwd)
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}
	cfgPath := filepath.Join(root, "cockpit.config.json")
	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("%s already exists — remove it first if you want to regenerate", cfgPath)
	}

	owner, name := "", ""
	if url, err := gitOriginURL(ctx, root); err == nil {
		owner, name = parseOwnerRepo(url)
	}

	raw := config.Raw{
		Repo: config.RepoRef{Owner: owner, Name: name},
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
	fmt.Printf("wrote %s (owner=%q name=%q — fill in trust.tiers before use)\n", cfgPath, owner, name)
	return nil
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
	fmt.Printf("bound to:       %s/%s (verified against origin remote)\n", cfg.Raw.Repo.Owner, cfg.Raw.Repo.Name)
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
	case !cfg.Raw.Git.CommitEnabled():
		fmt.Printf("git:            commits OFF — records are signed and registered, files are not committed,\n")
		fmt.Printf("                and fotons carry no locators (no commit exists for a permalink to pin)\n")
	case !cfg.Raw.Git.PushEnabled():
		fmt.Printf("git:            commits on, push OFF — permalinks are built and correct, but do not\n")
		fmt.Printf("                resolve until someone pushes\n")
	default:
		fmt.Printf("git:            commit + push\n")
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
