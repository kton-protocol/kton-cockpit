package main

// workflow.go is the command line for installing packages and working with workflows. Each command
// prints for a person by default and the full answer with --json.
//
//	cockpit install <package-dir>
//	cockpit workflow propose <result>...
//	cockpit workflow extract <result>... --name N --reference RUN [--hole NAME=STEP/SLOT]... [--param NAME=STEP/POS]...
//	cockpit workflow list
//	cockpit workflow show <name>
//	cockpit workflow run <name> [--check] [--bind NAME=VALUE]... [--dir DIR]

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/gitmick/ktonpkg"

	"github.com/kton-protocol/kton-cockpit/cockpit"
)

type cliArgs struct {
	pos   []string
	multi map[string][]string
	flags map[string]bool
}

// parseArgs splits positional arguments from --name value pairs and bare --flags. boolFlags names
// the flags that take no value.
func parseArgs(args []string, boolFlags ...string) (cliArgs, error) {
	a := cliArgs{multi: map[string][]string{}, flags: map[string]bool{}}
	isBool := map[string]bool{"--json": true}
	for _, b := range boolFlags {
		isBool[b] = true
	}
	for i := 0; i < len(args); i++ {
		x := args[i]
		switch {
		case isBool[x]:
			a.flags[x] = true
		case strings.HasPrefix(x, "--"):
			if i+1 >= len(args) {
				return a, fmt.Errorf("%s needs a value", x)
			}
			a.multi[x] = append(a.multi[x], args[i+1])
			i++
		default:
			a.pos = append(a.pos, x)
		}
	}
	return a, nil
}

func (a cliArgs) one(name string) string {
	if v := a.multi[name]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func short(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// ---- install ---------------------------------------------------------------------------------------

func runInstall(ctx context.Context, args []string) error {
	a, err := parseArgs(args)
	if err != nil || len(a.pos) != 1 {
		return fmt.Errorf("usage: cockpit install <package-dir> [--json]")
	}
	out, err := cockpit.New(cockpit.Start{}).Install(ctx, cockpit.InstallRequest{Path: a.pos[0]})
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if a.flags["--json"] {
		return emitJSON(out)
	}
	state := "installed"
	if out.AlreadyInstalled {
		state = "already installed"
	}
	fmt.Printf("%s %s, revision %s  →  %s\n", state, out.Name, out.Label, out.Dir)
	fmt.Printf("  package   %s\n", out.Package)
	fmt.Printf("  profile   %s\n", strings.TrimPrefix(out.Profile, "https://kton.dev/profile/"))
	fmt.Printf("  sealed    %s (head %s)\n", short(out.Seal), short(out.Head))
	if out.Ray != "" {
		fmt.Printf("  ray       %s\n", out.Ray)
	}
	fmt.Printf("  scope     %s@%s (%s)\n", out.Name, out.Label, short(out.Scope))
	if out.ClaimID != "" {
		fmt.Printf("  recorded  installed, claim %s\n", short(out.ClaimID))
	}
	if strings.HasSuffix(out.Profile, "/templates") {
		allowed := "no — its templates and queries are not in use"
		if out.Allowed {
			allowed = "yes — its templates and queries are in use"
		}
		fmt.Printf("  allowed   %s\n", allowed)
	}
	if out.Note != "" {
		fmt.Printf("\n  note: %s\n", out.Note)
	}
	return nil
}

// ---- workflow --------------------------------------------------------------------------------------

func runWorkflow(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cockpit workflow propose|extract|list|show|run …")
	}
	c := cockpit.New(cockpit.Start{})
	switch args[0] {
	case "propose":
		return workflowPropose(ctx, c, args[1:])
	case "extract":
		return workflowExtract(ctx, c, args[1:])
	case "list":
		return workflowList(ctx, c, args[1:])
	case "show":
		return workflowShow(ctx, c, args[1:])
	case "run":
		return workflowRun(ctx, c, args[1:])
	}
	return fmt.Errorf("unknown workflow command %q (propose, extract, list, show, run)", args[0])
}

func workflowPropose(ctx context.Context, c *cockpit.Cockpit, args []string) error {
	a, err := parseArgs(args)
	if err != nil || len(a.pos) == 0 {
		return fmt.Errorf("usage: cockpit workflow propose <result>... [--json]")
	}
	out, err := c.WorkflowPropose(ctx, a.pos)
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if a.flags["--json"] {
		return emitJSON(out)
	}
	p := out.Proposal
	fmt.Printf("%d run(s) behind %s\n", len(p.Runs), strings.Join(a.pos, ", "))
	for _, r := range p.Runs {
		var ends []string
		for _, e := range r.Endpoints {
			if f := out.Endpoints[e]; f != "" {
				ends = append(ends, f)
			}
		}
		fmt.Printf("  %s  ends in %s  (%d executions)\n", r.ID, strings.Join(ends, ", "), len(r.Executions))
	}
	fmt.Printf("  (--reference takes a run or the result it ended in)\n")
	fmt.Printf("\nsteps\n")
	for _, s := range p.Steps {
		fmt.Printf("  %-18s %s\n", s.ID, s.CommandFile)
	}
	fmt.Printf("\ncould be holes (--hole NAME=STEP/SLOT)\n")
	for _, f := range p.Files {
		if f.CommandFile {
			continue
		}
		vary := "same in every run"
		if distinct(f.Hashes) > 1 {
			vary = "differs between runs"
		}
		fmt.Printf("  %s/%s  — %s\n", f.Step, f.Slot, vary)
	}
	if len(p.Params) > 0 {
		fmt.Printf("\ncould be parameters (--param NAME=STEP/POSITION)\n")
		for _, pc := range p.Params {
			var vals []string
			for run, v := range pc.Values {
				vals = append(vals, run+": "+v)
			}
			sort.Strings(vals)
			fmt.Printf("  %s/%d  — %s\n", pc.Step, pc.Position, strings.Join(vals, ", "))
		}
	}
	if len(p.Conflicts) > 0 {
		fmt.Printf("\nconflicts (extract will refuse)\n")
		for _, x := range p.Conflicts {
			fmt.Printf("  %s\n", x)
		}
	}
	return nil
}

func distinct(m map[string]string) int {
	seen := map[string]bool{}
	for _, v := range m {
		seen[v] = true
	}
	return len(seen)
}

func workflowExtract(ctx context.Context, c *cockpit.Cockpit, args []string) error {
	a, err := parseArgs(args)
	usage := fmt.Errorf("usage: cockpit workflow extract <result>... --name NAME --reference RUN " +
		"[--hole NAME=STEP/SLOT]... [--param NAME=STEP/POSITION]... [--json]")
	if err != nil || len(a.pos) == 0 || a.one("--name") == "" || a.one("--reference") == "" {
		return usage
	}
	ch := ktonpkg.Choice{Name: a.one("--name"), Reference: a.one("--reference")}
	for _, h := range a.multi["--hole"] {
		name, where, ok := strings.Cut(h, "=")
		step, slot, ok2 := strings.Cut(where, "/")
		if !ok || !ok2 {
			return fmt.Errorf("--hole wants NAME=STEP/SLOT, got %q", h)
		}
		ch.Holes = append(ch.Holes, ktonpkg.HoleChoice{Name: name, Step: step, Slot: slot})
	}
	for _, p := range a.multi["--param"] {
		name, where, ok := strings.Cut(p, "=")
		step, pos, ok2 := strings.Cut(where, "/")
		n, perr := strconv.Atoi(pos)
		if !ok || !ok2 || perr != nil {
			return fmt.Errorf("--param wants NAME=STEP/POSITION, got %q", p)
		}
		ch.Params = append(ch.Params, ktonpkg.ParamChoice{Name: name, Step: step, Position: n})
	}
	out, err := c.WorkflowExtract(ctx, cockpit.WorkflowExtractRequest{Refs: a.pos, Choice: ch})
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if a.flags["--json"] {
		return emitJSON(out)
	}
	fmt.Printf("extracted %s  →  %s\n", ch.Name, out.Dir)
	fmt.Printf("  package     %s\n", out.Package)
	fmt.Printf("  ray         %s\n", out.Ray)
	fmt.Printf("  holes       %s\n", strings.Join(out.Holes, ", "))
	fmt.Printf("  parameters  %s\n", strings.Join(out.Params, ", "))
	fmt.Printf("  reference   %d run(s) of %s packed with it\n", len(out.References), ch.Reference)
	fmt.Printf("  sealed      %s\n", short(out.Seal))
	return nil
}

func workflowList(ctx context.Context, c *cockpit.Cockpit, args []string) error {
	a, err := parseArgs(args)
	if err != nil || len(a.pos) != 0 {
		return fmt.Errorf("usage: cockpit workflow list [--json]")
	}
	out, err := c.WorkflowList(ctx)
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if a.flags["--json"] {
		return emitJSON(out)
	}
	if len(out.Workflows) == 0 {
		fmt.Println("no workflows installed")
		return nil
	}
	fmt.Printf("%d workflow(s) installed  (query from %s)\n\n", len(out.Workflows), out.Query)
	for _, w := range out.Workflows {
		fmt.Printf("  %-16s revision %-4s ray %s  sealed %s\n", w["name"], w["label"], short(w["ray"]), short(w["seal"]))
	}
	return nil
}

func workflowShow(ctx context.Context, c *cockpit.Cockpit, args []string) error {
	a, err := parseArgs(args)
	if err != nil || len(a.pos) != 1 {
		return fmt.Errorf("usage: cockpit workflow show <name> [--json]")
	}
	out, err := c.WorkflowShow(ctx, a.pos[0])
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if a.flags["--json"] {
		return emitJSON(out)
	}
	fmt.Printf("%s, revision %s  (%s)\n", out.Name, out.Label, out.Dir)
	fmt.Printf("  ray     %s\n  sealed  %s\n\n", out.Ray, short(out.Seal))
	fmt.Println("steps")
	for i, s := range out.Steps {
		fmt.Printf("  %d. %-16s %s\n", i+1, s.ID, s.Command)
		if s.Label != "" && s.Label != s.ID {
			fmt.Printf("     %s\n", s.Label)
		}
		if s.Rationale != "" {
			fmt.Printf("     why: %s\n", s.Rationale)
		}
		fmt.Printf("     in:  %s\n", strings.Join(s.Inputs, ", "))
		if r := out.References[s.ID]; len(r) > 0 {
			fmt.Printf("     out: %s  (reference packed)\n", strings.Join(r, ", "))
		}
	}
	fmt.Println("\nholes (--bind NAME=VALUE)")
	for _, h := range out.Holes {
		req := "optional"
		if h.Required {
			req = "required"
		}
		def := ""
		if h.Default != "" {
			def = "  test data: " + h.Default
		}
		fmt.Printf("  %-12s %-6s %s%s\n", h.Name, h.Type, req, def)
	}
	return nil
}

func workflowRun(ctx context.Context, c *cockpit.Cockpit, args []string) error {
	a, err := parseArgs(args, "--check")
	if err != nil || len(a.pos) != 1 {
		return fmt.Errorf("usage: cockpit workflow run <name> [--check] [--bind NAME=VALUE]... [--dir DIR] [--json]")
	}
	req := cockpit.WorkflowRunRequest{Name: a.pos[0], Check: a.flags["--check"], Dir: a.one("--dir"),
		Bindings: map[string]string{}}
	for _, b := range a.multi["--bind"] {
		k, v, ok := strings.Cut(b, "=")
		if !ok {
			return fmt.Errorf("--bind wants NAME=VALUE, got %q", b)
		}
		req.Bindings[k] = v
	}
	out, err := c.WorkflowRun(ctx, req)
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if a.flags["--json"] {
		return emitJSON(out)
	}
	var bs []string
	for k, v := range out.Bindings {
		bs = append(bs, k+"="+v)
	}
	sort.Strings(bs)
	fmt.Printf("ran %s in %s  (%s)\n", out.Name, out.Dir, strings.Join(bs, ", "))
	for _, s := range out.Steps {
		line := fmt.Sprintf("  %-16s foton %s", s.ID, short(s.FotonID))
		if req.Check {
			switch {
			case s.Level != "" && s.SameRecord:
				line += fmt.Sprintf("  reproduces the reference (%s, same record, co-signed)", s.Level)
			case s.Level != "":
				line += fmt.Sprintf("  reproduces the reference (%s)", s.Level)
			default:
				line += "  no reference for this step"
			}
		}
		fmt.Println(line)
	}
	return nil
}
