package cli

// shellsay.go is `cockpit say` typed at a shell: the template, what the claim is about, and its
// fields as name=value. Same SayRequest, same handler as the JSON form; the template ceiling and the
// reproduction precondition are the handler's, untouched.
//
//	cockpit say                                          the templates, their fields
//	cockpit say working-on runs/means step=cleaning by-session=alice
//	cockpit say working-on out/clean.csv step=checking by-session=alice
//	cockpit say reproduces runs/check                    a run made with run new --from sha256:…

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func sayOverview(ctx context.Context, cfg *config.Config) {
	fmt.Println("cockpit say <template> <run|file|sha256:id> [field=value]... [--scope NAME] [--json]")
	fmt.Println()
	ts := readTemplates(cfg)
	if len(ts) == 0 {
		fmt.Println("  no templates allowed here (claims.allowedTemplates, files in the template directory)")
	}
	for _, t := range ts {
		fmt.Printf("  %-14s about %s\n", t.Name, targetWords[orDefault(t.Target, "foton")])
		if t.Name == "reproduces" {
			fmt.Println("                 cockpit say reproduces runs/<slug>   — a run made with run new --from sha256:…;")
			fmt.Println("                 the cockpit compares the bytes and sets level and reproducedBy itself")
			continue
		}
		for _, name := range sortedFieldNames(t) {
			f := t.Fields[name]
			req := ""
			if f.Required {
				req = " (required)"
			}
			if name == identityField {
				fmt.Printf("                 %s=<%s>  filled in: %s (your identity)\n", name, f.Type, cfg.Raw.Identity.SessionID)
				continue
			}
			vals := ""
			if len(f.Values) > 0 {
				vals = " one of " + strings.Join(f.Values, "|")
			}
			fmt.Printf("                 %s=<%s>%s%s\n", name, f.Type, vals, req)
			if used := usedValues(ctx, cfg, t.Name, name); len(used) > 0 {
				fmt.Printf("                   used so far: %s\n", strings.Join(used, " · "))
			}
		}
	}
	fmt.Println()
	fmt.Println("a run folder stands for the record its run made; a file for its bytes.")
}

func shellSay(ctx context.Context, args []string) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	if len(args) == 0 {
		sayOverview(ctx, cfg)
		return nil
	}
	in := cockpit.SayRequest{Template: args[0], Fields: map[string]string{}}
	asJSON := false
	var pos []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			asJSON = true
		case a == "--scope":
			if i+1 >= len(args) {
				return fmt.Errorf("--scope needs a name")
			}
			in.Scope = args[i+1]
			i++
		case strings.HasPrefix(a, "--"):
			return fmt.Errorf("unknown flag %s — cockpit say lists the templates", a)
		case strings.Contains(a, "=") && len(pos) > 0:
			k, v, _ := strings.Cut(a, "=")
			in.Fields[k] = v
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: cockpit say %s <run|file|sha256:id> [field=value]...", in.Template)
	}

	if in.Template == "reproduces" {
		if err := reproducesFromRun(ctx, cfg, pos[0], &in); err != nil {
			return err
		}
		in.Fields = nil
	} else {
		id, note, err := resolveRef(ctx, cfg, pos[0])
		if err != nil {
			return err
		}
		if note != "" {
			fmt.Printf("(%s)\n", note)
		}
		in.Subject = id
		fillFromIdentity(cfg, in.Template, in.Fields)
		if err := checkFields(cfg, in.Template, in.Fields); err != nil {
			if used := usedValues(ctx, cfg, in.Template, "step"); len(used) > 0 && in.Fields["step"] == "" {
				return fmt.Errorf("%v\n  steps used here so far: %s", err, strings.Join(used, " · "))
			}
			return err
		}
		if len(in.Fields) == 0 {
			in.Fields = nil
		}
	}

	out, err := cockpit.New(cockpit.Start{}).Say(ctx, in)
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(out)
	}
	fmt.Printf("claim    %s\n", out.ClaimID)
	fmt.Printf("says     %s about %s\n", in.Template, in.Subject)
	if out.Level != "" {
		fmt.Printf("level    %s (measured here, not stated)\n", out.Level)
	}
	return nil
}

// checkFields says which fields a template needs before the handler would refuse without naming
// them all. Unknown names are refused the same way: a typo'd field is a claim saying less than meant.
func checkFields(cfg *config.Config, template string, given map[string]string) error {
	for _, t := range readTemplates(cfg) {
		if t.Name != template {
			continue
		}
		var missing, unknown []string
		for _, name := range sortedFieldNames(t) {
			if _, ok := given[name]; !ok && t.Fields[name].Required {
				missing = append(missing, name+"=<"+t.Fields[name].Type+">")
			}
		}
		for k := range given {
			if _, ok := t.Fields[k]; !ok {
				unknown = append(unknown, k)
			}
		}
		sort.Strings(unknown)
		switch {
		case len(unknown) > 0:
			return fmt.Errorf("template %s has no field %s (it has: %s)", template, strings.Join(unknown, ", "), strings.Join(sortedFieldNames(t), ", "))
		case len(missing) > 0:
			return fmt.Errorf("template %s also needs %s", template, strings.Join(missing, " "))
		}
		return nil
	}
	return nil // not a template read here (e.g. from a package); the handler judges it
}

// reproducesFromRun fills a reproduces claim from a run folder made from somebody's record: their
// record from RUN.md, this run's record, and an output whose bytes both name.
func reproducesFromRun(ctx context.Context, cfg *config.Config, ref string, in *cockpit.SayRequest) error {
	if strings.HasPrefix(ref, "sha256:") {
		return fmt.Errorf("say reproduces takes the run folder that reproduced it (runs/<slug>, made with run new --from %s);\n"+
			"  for anything else use the JSON form: cockpit say --help", ref)
	}
	abs, _ := filepath.Abs(ref)
	rel, err := filepath.Rel(cfg.RepoRoot, abs)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	theirs := reproducesOf(filepath.Join(abs, "RUN.md"))
	if theirs == "" {
		return fmt.Errorf("%s was not made from a record (its RUN.md names none) — make one with: cockpit run new <slug> --from sha256:<record>", ref)
	}
	mine, err := runFoton(ctx, cfg, rel)
	if err != nil {
		return err
	}
	r := binaries.New(cfg)
	their, err := r.FotonByID(ctx, theirs)
	if err != nil {
		return err
	}
	my, err := r.FotonByID(ctx, mine)
	if err != nil {
		return err
	}
	for _, o := range their.Outputs {
		for _, m := range my.Outputs {
			if o.Hash == m.Hash {
				in.Subject, in.SubjectOutputHash, in.ReproducedOutput, in.ReproducedFotonID = theirs, o.Hash, m.Path, mine
				fmt.Printf("(%s reproduces %s: %s has the bytes of its %s)\n", rel, short16(theirs), m.Path, o.Path)
				return nil
			}
		}
	}
	return fmt.Errorf("%s did not produce the bytes %s records — nothing to claim", rel, short16(theirs))
}

func sortedFieldNames(t templateInfo) []string {
	names := make([]string, 0, len(t.Fields))
	for n := range t.Fields {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

var targetWords = map[string]string{
	"foton":  "a record (a run)",
	"either": "a run or a file",
	"claim":  "a claim",
	"":       "anything",
}

// identityField names who is speaking. A claim is signed, so the signature already says it; with
// the one identity this repository signs as, asking the person to type it again only invites a
// value that disagrees with the signature. Given explicitly, it is kept.
const identityField = "by-session"

func fillFromIdentity(cfg *config.Config, template string, fields map[string]string) {
	for _, t := range readTemplates(cfg) {
		if _, has := t.Fields[identityField]; t.Name == template && has && fields[identityField] == "" {
			fields[identityField] = cfg.Raw.Identity.SessionID
		}
	}
}

// usedValues are the values a template's field has had in verified claims here — the step names
// already in use, so the next claim can say the same step the same way.
func usedValues(ctx context.Context, cfg *config.Config, template, field string) []string {
	pred := predicateOf(cfg, template)
	if pred == template {
		return nil
	}
	ans, err := cockpit.New(cockpit.Start{}).Ask(ctx, cockpit.AskRequest{Query: "by", Axis: "predicate", Ref: pred})
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range ans.Claims {
		if m, ok := c.Object.(map[string]any); ok {
			if v, ok := m[field].(string); ok && v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	sort.Strings(out)
	return out
}
