package cli

// shellask.go is `cockpit ask` typed at a shell: the query and the reference as words, the answer
// as lines a person reads. It builds the same AskRequest the JSON form takes and calls the same
// handler; --json prints the full answer instead.
//
//	cockpit ask                                   the questions there are
//	cockpit ask producer out/clean.csv
//	cockpit ask about runs/means
//	cockpit ask by signer me
//	cockpit ask reproductions out/clean.csv --min 2 --tier peer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

var askQueries = []struct{ name, arg, what string }{
	{"record", "<run|file|sha256:id>", "what a record says: command, inputs, outputs, environment, who signed"},
	{"producer", "<file|sha256:hash>", "the runs that made these bytes"},
	{"uses", "<file|sha256:hash>", "the runs that took these bytes as input"},
	{"lineage", "<file|sha256:hash>", "the chain of runs behind these bytes"},
	{"reproductions", "<file|sha256:hash>", "how many independent parties produced these bytes"},
	{"about", "<run|file|sha256:id>", "the claims about a run, a file or a record"},
	{"by", "signer|predicate|object <value>", "claims by a signer (me), of a kind (a template name), or naming a value"},
	{"scope", "<sha256:scope-id>", "a scope's chain: head, length, whether it branched"},
}

func askOverview() {
	fmt.Println("cockpit ask <question> <reference> [--tier T] [--signer KEYID] [--level L0|L1|L2] [--scope ID] [--min N] [--json]")
	fmt.Println()
	for _, q := range askQueries {
		fmt.Printf("  %-14s %-34s %s\n", q.name, q.arg, q.what)
	}
	fmt.Println()
	fmt.Println("a reference is a file (its bytes), a run folder (runs/<slug>: the record its run made), or sha256:…")
	fmt.Println("only records a key in your trust tiers verifies are answered; the rest are counted, never shown.")
}

func shellAsk(ctx context.Context, args []string) error {
	if len(args) == 0 {
		askOverview()
		return nil
	}
	in := cockpit.AskRequest{Query: args[0]}
	var filter cockpit.AskFilter
	asJSON := false
	var pos []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		val := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch a {
		case "--json":
			asJSON = true
		case "--tier":
			filter.TrustTier, err = val()
		case "--signer":
			filter.Signer, err = val()
		case "--level":
			filter.Level, err = val()
		case "--scope":
			filter.Scope, err = val()
		case "--min":
			var v string
			if v, err = val(); err == nil {
				filter.MinReproductions, err = strconv.Atoi(v)
			}
		default:
			if strings.HasPrefix(a, "--") {
				return fmt.Errorf("unknown flag %s — cockpit ask lists the questions", a)
			}
			pos = append(pos, a)
		}
		if err != nil {
			return err
		}
	}
	if filter != (cockpit.AskFilter{}) {
		in.Filter = &filter
	}

	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	if in.Query == "by" {
		if len(pos) != 2 {
			return fmt.Errorf("usage: cockpit ask by signer|predicate|object <value>   (cockpit ask by signer me)")
		}
		in.Axis, in.Ref = pos[0], pos[1]
		switch {
		case in.Axis == "signer" && in.Ref == "me":
			if in.Ref, err = ownClaimsKeyID(cfg); err != nil {
				return err
			}
		case in.Axis == "predicate" && !strings.Contains(in.Ref, ":"):
			in.Ref = predicateOf(cfg, in.Ref)
		}
	} else {
		if len(pos) != 1 {
			for _, q := range askQueries {
				if q.name == in.Query {
					return fmt.Errorf("usage: cockpit ask %s %s", q.name, q.arg)
				}
			}
			return fmt.Errorf("no question %q — cockpit ask lists them", in.Query)
		}
		in.Ref = pos[0]
		// A run folder stands for its record. A file is left to ask, which hashes it itself and
		// says so (resolvedFrom); a record id is taken as it is.
		if fi, serr := os.Stat(in.Ref); serr == nil && fi.IsDir() {
			id, note, rerr := resolveRef(ctx, cfg, in.Ref)
			if rerr != nil {
				return rerr
			}
			fmt.Printf("(%s)\n", note)
			in.Ref = id
		} else if serr == nil && (in.Query == "record" || in.Query == "about") {
			// record wants a record; about over a file asks about its bytes.
			id, note, rerr := resolveRef(ctx, cfg, in.Ref)
			if rerr != nil {
				return rerr
			}
			if in.Query == "record" {
				ans, aerr := cockpit.New(cockpit.Start{}).Ask(ctx, cockpit.AskRequest{Query: "producer", Ref: in.Ref})
				if aerr == nil && len(ans.Included) > 0 {
					id = ans.Included[0]
					note = in.Ref + " → the record that made it " + short16(id)
				}
			}
			fmt.Printf("(%s)\n", note)
			in.Ref = id
		}
	}

	out, err := cockpit.New(cockpit.Start{}).Ask(ctx, in)
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(out)
	}
	printAnswer(ctx, cfg, out)
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// predicateOf maps a template name to its predicate, so `ask by predicate working-on` works.
func predicateOf(cfg *config.Config, name string) string {
	for _, t := range readTemplates(cfg) {
		if t.Name == name {
			return t.Predicate
		}
	}
	return name
}

func printAnswer(ctx context.Context, cfg *config.Config, out *cockpit.AskResult) {
	tier := map[string]string{}
	for _, r := range out.Records {
		if r.Verified {
			tier[r.ID] = r.Tier
		}
	}
	r := binaries.New(cfg)
	if out.ResolvedFrom != "" {
		fmt.Printf("(%s → its bytes %s)\n", out.ResolvedFrom, short16(out.Ref))
	}

	switch out.Query {
	case "record":
		for _, id := range out.Included {
			rec, err := r.FotonByID(ctx, id)
			if err != nil {
				fmt.Printf("%s  [%s]\n", id, tier[id])
				continue
			}
			fmt.Printf("record   %s\n", id)
			fmt.Printf("signed   %s (tier %s)\n", shortID(rec.SignerKeyID), tier[id])
			fmt.Printf("command  %s\n", rec.Cmd)
			if rec.EnvRef != "" {
				fmt.Printf("ran in   %s\n", rec.EnvRef)
			}
			for _, f := range rec.Inputs {
				fmt.Printf("  in   %-48s %s\n", f.Path, short16(f.Hash))
			}
			for _, f := range rec.Outputs {
				fmt.Printf("  out  %-48s %s\n", f.Path, short16(f.Hash))
			}
		}
	case "producer", "uses", "lineage":
		for _, f := range out.Fotons {
			cmd := ""
			if rec, err := r.FotonByID(ctx, f.ID); err == nil {
				cmd = rec.Cmd
			}
			fmt.Printf("%s  %-8s %s\n", short16(f.ID), tier[f.ID], cmd)
		}
	case "reproductions":
		fmt.Printf("↻%d — %d independent verified signer(s) produced these bytes\n", out.VerifiedSigners, out.VerifiedSigners)
		for _, f := range out.Fotons {
			fmt.Printf("  %s  %s\n", short16(f.ID), tier[f.ID])
		}
	case "about", "by":
		names := map[string]string{}
		for _, t := range readTemplates(cfg) {
			names[t.Predicate] = t.Name
		}
		for _, c := range out.Claims {
			what := names[c.Predicate]
			if what == "" {
				what = c.Predicate
			}
			obj, _ := json.Marshal(c.Object)
			fmt.Printf("%s  %-8s %-12s about %s  %s", short16(c.ID), tier[c.ID], what, short16(c.Subject), string(obj))
			if c.When != "" {
				fmt.Printf("  (%s)", c.When)
			}
			fmt.Println()
		}
	default:
		fmt.Println(out.Raw)
	}
	if len(out.Included) == 0 && out.Query != "scope" {
		fmt.Println("nothing verified answers this")
	}
	if n := len(out.Excluded); n > 0 {
		fmt.Printf("%d record(s) not shown: no key in your trust tiers verifies them\n", n)
	}
}

type templateInfo struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Predicate string `json:"predicate"`
	Fields    map[string]struct {
		Type     string   `json:"type"`
		Required bool     `json:"required"`
		Values   []string `json:"values"`
	} `json:"fields"`
}

// readTemplates reads the allowed templates in this repository's template directory.
func readTemplates(cfg *config.Config) []templateInfo {
	var out []templateInfo
	for _, name := range cfg.Raw.Claims.AllowedTemplates {
		b, err := os.ReadFile(filepath.Join(cfg.TemplatesDir, name+".json"))
		if err != nil {
			continue
		}
		var t templateInfo
		if json.Unmarshal(b, &t) == nil {
			if t.Name == "" {
				t.Name = name
			}
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
