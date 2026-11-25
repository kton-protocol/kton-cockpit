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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func sayOverview(ctx context.Context, cfg *config.Config) {
	fmt.Println("cockpit say <template> <run|file|sha256:id> [field=value]... [--scope NAME [--expect-head ID]] [--expect-template HASH] [--expect-revision ID] [--expect-predicate IRI] [--as NAME] [--json]")
	fmt.Println()
	ts := readTemplates(cfg)
	if len(ts) == 0 {
		fmt.Println("  no templates allowed here (claims.allowedTemplates with files in the template directory, or claims.allowedPackages)")
	}
	for _, t := range ts {
		fmt.Printf("  %-14s about %s\n", t.Name, targetWords[orDefault(t.Target, "foton")])
		if t.Name == "reproduces" {
			fmt.Println("                 cockpit say reproduces work/<slug>   — a run made with run new --from sha256:…;")
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
				fmt.Printf("                 %s=<%s>  filled in: %s (your identity)\n", name, f.Type, cfg.Identity.SessionID)
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

// Template is one entry of `say --json`: an allowed template, what it is about, and its fields.
// A field say fills itself names what it fills in (filledBy, the identity it signs as).
type Template struct {
	Name  string `json:"name"`
	About string `json:"about"`
	// Predicate and Context are as say will sign them: resolved through the template's package aliases
	// and the repository's alias file (kton-review R1b on #177).
	Predicate string `json:"predicate,omitempty"`
	Context   string `json:"context,omitempty"`
	// Hash is the sha256 of the template's bytes as its source holds them, Revision the package revision
	// it came from: what say --expect-template and --expect-revision take. Package names the package a
	// package template came from; it is absent for a template the repository holds itself.
	Package  string `json:"package,omitempty"`
	Hash     string `json:"hash,omitempty"`
	Revision string `json:"revision,omitempty"`
	// SignerRoles (role IRIs) and Scopes (seed IRIs, template.ScopeIRIPrefix + the seed claim's id) are
	// the template's declared intent (kton #301): who a claim from it is meant to be signed by and in which
	// scopes. Advisory only: say neither checks nor enforces them. Absent when the template declares none;
	// absent Scopes means any scope.
	SignerRoles []string                 `json:"signerRoles,omitempty"`
	Scopes      []string                 `json:"scopes,omitempty"`
	Fields      map[string]TemplateField `json:"fields"`
}

// TemplateField is one field of a template.
type TemplateField struct {
	Type     string   `json:"type"`
	Required bool     `json:"required,omitempty"`
	Values   []string `json:"values,omitempty"`
	FilledBy string   `json:"filledBy,omitempty"`
}

func sayListing(cfg *config.Config) ([]Template, error) {
	out := []Template{}
	set, origins, serr := binaries.New(cfg).TemplatesWithOrigins()
	if why := binaries.MisplacedAliases(cfg); why != "" {
		return nil, fmt.Errorf("%s", why)
	}
	for _, t := range readTemplates(cfg) {
		e := Template{Name: t.Name, About: orDefault(t.Target, "foton"), Predicate: t.Predicate, Fields: map[string]TemplateField{}}
		if st, ok := set.Get(t.Name); serr == nil && ok {
			e.Predicate = set.Resolve(st.Predicate)
			if st.Context != "" {
				e.Context = set.Resolve(st.Context)
			}
			o := origins[st.Name]
			e.Package, e.Hash, e.Revision = o.PackageName, o.Hash, o.Revision
			// Roles as IRIs, resolved like the predicate (kton-review F2 on #260); scopes are IRIs already.
			for _, r := range st.SignerRoles {
				e.SignerRoles = append(e.SignerRoles, set.Resolve(r))
			}
			e.Scopes = st.Scopes
		}
		for name, f := range t.Fields {
			tf := TemplateField{Type: f.Type, Required: f.Required, Values: f.Values}
			if name == identityField {
				tf.FilledBy = cfg.Identity.SessionID
			}
			e.Fields[name] = tf
		}
		out = append(out, e)
	}
	return out, nil
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
	if len(args) == 1 && args[0] == "--json" {
		ts, err := sayListing(cfg)
		if err != nil {
			return err
		}
		return printJSON(ts)
	}
	if args[0] == "--batch" {
		// The answer is JSON in any case; --json is taken, as say's other forms take it (qm-dev-provenance).
		if len(args) == 3 && args[2] == "--json" {
			args = args[:2]
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: cockpit say --batch <file.jsonl | ->   (one say request as JSON per line)")
		}
		in, err := readBatch(args[1])
		if err != nil {
			return err
		}
		out, err := cockpit.New(cockpit.Start{}).SayBatch(ctx, in)
		if err != nil {
			return err
		}
		return printJSON(out)
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
		case a == "--as":
			if i+1 >= len(args) {
				return fmt.Errorf("--as needs a key name of identity.keyring")
			}
			in.As = args[i+1]
			i++
		case a == "--expect-predicate":
			if i+1 >= len(args) {
				return fmt.Errorf("--expect-predicate needs the predicate say --json listed")
			}
			in.ExpectPredicate = args[i+1]
			i++
		case a == "--expect-template" || a == "--expect-revision":
			if i+1 >= len(args) {
				return fmt.Errorf("%s needs the template hash or package revision you showed", a)
			}
			if a == "--expect-template" {
				in.ExpectTemplate = args[i+1]
			} else {
				in.ExpectRevision = args[i+1]
			}
			i++
		case a == "--expect-head":
			if i+1 >= len(args) {
				return fmt.Errorf("--expect-head needs the head you saw (sha256:…)")
			}
			in.ExpectHead = args[i+1]
			i++
		case strings.HasPrefix(strings.TrimSpace(a), "{"):
			return fmt.Errorf("flags do not combine with the JSON form; it carries them as fields, e.g. {\"as\": \"NAME\", \"scope\": \"NAME\", …} — cockpit say --help lists them")
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
			fmt.Fprintf(os.Stderr, "(%s)\n", note)
		}
		in.Subject = id
		// A file goes to the core as its path, which the core resolves to the same hash with the same
		// resolver, so it knows which file the claim is about and keeps its bytes (B11).
		if strings.HasPrefix(id, "sha256:") && !strings.HasPrefix(pos[0], "sha256:") {
			if abs, err := filepath.Abs(pos[0]); err == nil {
				if fi, err := os.Stat(abs); err == nil && fi.Mode().IsRegular() {
					in.Subject = abs
				}
			}
		}
		fillFromIdentity(cfg, in.Template, in.As, in.Fields)
		if err := checkFields(cfg, in.Template, in.Fields); err != nil {
			// Nothing was signed: the cockpit declines the claim as it stands, which is a refusal.
			reason := err.Error()
			if used := usedValues(ctx, cfg, in.Template, "step"); len(used) > 0 && in.Fields["step"] == "" {
				reason += "\n  steps used here so far: " + strings.Join(used, " · ")
			}
			return &cockpit.Refusal{Code: "argument", Clause: "SPEC §8.1", Reason: reason}
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
	if out.Pending != nil {
		fmt.Printf("pending  %s: %s\n", out.Pending.Code, out.Pending.Reason)
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
		return fmt.Errorf("say reproduces takes the run folder that reproduced it (work/<slug>, made with run new --from %s);\n"+
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

// identityField is the field say fills from the identity it signs as (cockpit.FillIdentity).
const identityField = cockpit.IdentityField

func fillFromIdentity(cfg *config.Config, template, as string, fields map[string]string) {
	cockpit.FillIdentity(cfg, template, as, fields)
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

// readBatch reads `say --batch`'s input: one say request as JSON per line (blank lines skipped), from
// a file or from stdin ("-"). A line that is not a request is refused by its number, and nothing is said.
func readBatch(path string) (cockpit.SayBatchRequest, error) {
	var in cockpit.SayBatchRequest
	f := os.Stdin
	if path != "-" {
		var err error
		if f, err = os.Open(path); err != nil {
			return in, &cockpit.Refusal{Code: "argument", Clause: "SPEC §8.9", Reason: err.Error()}
		}
		defer f.Close()
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var one cockpit.SayRequest
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&one); err != nil {
			return in, &cockpit.Refusal{Code: "argument", Clause: "SPEC §8.9", Reason: fmt.Sprintf("line %d is not a say request: %v", n, err)}
		}
		in.Claims = append(in.Claims, one)
	}
	if err := sc.Err(); err != nil {
		return in, &cockpit.Refusal{Code: "argument", Clause: "SPEC §8.9", Reason: err.Error()}
	}
	return in, nil
}
