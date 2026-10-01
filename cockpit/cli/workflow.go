package cli

// workflow.go is the command line for installing packages and working with workflows. Each command
// prints for a person by default and the full answer with --json.
//
//	cockpit install <package-dir> [--allow]
//	cockpit workflow propose <result>...
//	cockpit workflow extract <result>... --name NAME --reference RESULT [--hole NAME=N]... [--param NAME=N]...
//	cockpit workflow list
//	cockpit workflow show <name>
//	cockpit workflow run <name> [--check] [--bind NAME=VALUE]... [--dir DIR]
//	cockpit workflow trace <result>

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
	a, err := parseArgs(args, "--allow")
	if err != nil || len(a.pos) != 1 {
		return fmt.Errorf("usage: cockpit install <package-dir> [--allow] [--json]")
	}
	out, err := cockpit.New(cockpit.Start{}).Install(ctx, cockpit.InstallRequest{Path: a.pos[0], Allow: a.flags["--allow"]})
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
		if out.AllowedNow {
			allowed = "yes, added to claims.allowedPackages — its templates and queries are in use"
		} else if out.Allowed {
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
		return fmt.Errorf("usage: cockpit workflow propose|extract|list|show|run|trace …")
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
	case "trace":
		return workflowTrace(ctx, c, args[1:])
	}
	return fmt.Errorf("unknown workflow command %q (propose, extract, list, show, run, trace)", args[0])
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
	fmt.Printf("\ncandidates — choose with --hole NAME=N (files) and --param NAME=N (command-line places)\n")
	for _, c := range cockpit.Candidates(p) {
		var vals []string
		for run, v := range c.Values {
			if c.Kind == "file" {
				v = short(v)
			}
			vals = append(vals, run+": "+v)
		}
		sort.Strings(vals)
		vary := ""
		if c.Kind == "file" && distinct(c.Values) == 1 {
			vary = "  (same in every run)"
		}
		where := c.Step + "/" + c.Slot
		if c.Kind == "param" {
			where = fmt.Sprintf("%s, argument %d", c.Step, c.Position)
		}
		fmt.Printf("  [%d] %-5s %-28s %s%s\n", c.N, c.Kind, where, strings.Join(vals, ", "), vary)
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
	usage := fmt.Errorf("usage: cockpit workflow extract <result>... --name NAME --reference RESULT " +
		"[--hole NAME=N]... [--param NAME=N]... [--json]   (N: the numbers propose prints)")
	if err != nil || len(a.pos) == 0 || a.one("--name") == "" || a.one("--reference") == "" {
		return usage
	}
	ch := ktonpkg.Choice{Name: a.one("--name"), Reference: a.one("--reference")}
	var picks []cockpit.Pick
	for kind, flag := range map[string]string{"hole": "--hole", "param": "--param"} {
		for _, v := range a.multi[flag] {
			name, where, ok := strings.Cut(v, "=")
			if !ok {
				return fmt.Errorf("%s wants NAME=N (a number from propose), got %q", flag, v)
			}
			if n, err := strconv.Atoi(where); err == nil {
				picks = append(picks, cockpit.Pick{Name: name, Kind: kind, Number: n})
				continue
			}
			step, rest, ok := strings.Cut(where, "/")
			if !ok {
				return fmt.Errorf("%s wants NAME=N, or NAME=STEP/%s, got %q", flag, map[string]string{"hole": "SLOT", "param": "POSITION"}[kind], v)
			}
			if kind == "hole" {
				ch.Holes = append(ch.Holes, ktonpkg.HoleChoice{Name: name, Step: step, Slot: rest})
			} else {
				n, err := strconv.Atoi(rest)
				if err != nil {
					return fmt.Errorf("--param wants NAME=N or NAME=STEP/POSITION, got %q", v)
				}
				ch.Params = append(ch.Params, ktonpkg.ParamChoice{Name: name, Step: step, Position: n})
			}
		}
	}
	out, err := c.WorkflowExtract(ctx, cockpit.WorkflowExtractRequest{Refs: a.pos, Choice: ch, Picks: picks})
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
	if out.InReferenceDir {
		fmt.Printf("  %s is where the reference ran: paths are part of a record's identity, so only there\n"+
			"  is the same work the same record — and your run signs the reference instead of standing beside it\n", out.Dir)
	}
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

func workflowTrace(ctx context.Context, c *cockpit.Cockpit, args []string) error {
	a, err := parseArgs(args)
	if err != nil || len(a.pos) != 1 {
		return fmt.Errorf("usage: cockpit workflow trace <result> [--json]")
	}
	out, err := c.WorkflowTrace(ctx, a.pos[0])
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if a.flags["--json"] {
		return emitJSON(out)
	}
	fmt.Printf("%s comes from %d step(s), each verified against this repo's trust tiers:\n", out.Ref, len(out.Steps))
	for i, s := range out.Steps {
		fmt.Printf("\n  %d. %-16s %s   (foton %s, signed by %s)\n", i+1, s.Step, s.Cmd, short(s.FotonID), strings.Join(s.Tiers, " + "))
		fmt.Printf("     in:  %s\n", strings.Join(s.Inputs, ", "))
		fmt.Printf("     out: %s\n", strings.Join(s.Outputs, ", "))
		if s.Reference != "" {
			fmt.Printf("     this is the reference run of %s\n", s.Reference)
		}
	}
	return nil
}
