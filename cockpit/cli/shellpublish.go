package cli

// shellpublish.go is `cockpit publish` typed at a shell: inputs and outputs as flags, the command
// after --. Same PublishRequest, same handler as the JSON form.
//
//	cockpit publish --in data/runs.csv --in clean.py --out out/clean.csv -- python3 clean.py
//	cockpit publish --in data/runs.csv --outputs-in out -- Rscript clean.R

import (
	"context"
	"fmt"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit"
)

func shellPublish(ctx context.Context, args []string) error {
	var in cockpit.PublishRequest
	asJSON := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			in.Cmd = shellJoin(args[i+1:])
			break
		}
		if a == "--json" {
			asJSON = true
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("%s needs a value", a)
		}
		v := args[i+1]
		i++
		switch a {
		case "--in":
			in.Inputs = append(in.Inputs, v)
		case "--out":
			in.Outputs = append(in.Outputs, v)
		case "--outputs-in":
			in.OutputsIn = v
		case "--output-dir":
			in.OutputDir = v
		case "--env-ref":
			in.EnvRef = v
		case "--corpus":
			in.Corpus = append(in.Corpus, v)
		default:
			return fmt.Errorf("unknown flag %s\n%s", a, publishUsage)
		}
	}
	if in.Cmd == "" {
		return fmt.Errorf("no command — it goes after --\n%s", publishUsage)
	}

	out, err := cockpit.New(cockpit.Start{}).Publish(ctx, in)
	exitIfRefused(err)
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(out)
	}
	if out.Stdout != "" {
		fmt.Println(indent(strings.TrimRight(out.Stdout, "\n"), "  | "))
	}
	fmt.Printf("record   %s\n", out.FotonID)
	if out.ExecutedIn != "" {
		fmt.Printf("ran in   %s\n", out.ExecutedIn)
	} else {
		fmt.Printf("ran      by you — recorded, not run here (no image pinned: cockpit pin <image>)\n")
	}
	for _, p := range sortedKeys(out.OutputHashes) {
		fmt.Printf("  out  %-48s %s\n", p, short16(out.OutputHashes[p]))
	}
	for _, p := range out.UndeclaredChanges {
		fmt.Printf("  also changed, not recorded: %s\n", p)
	}
	switch {
	case out.PushRejected:
		fmt.Println("committed; the push was rejected (someone pushed meanwhile): git pull --rebase && git push")
	case out.Pushed:
		fmt.Printf("committed and pushed (%s)\n", shortSHA(out.CommitSHA))
	case out.Committed:
		fmt.Printf("committed (%s), not pushed\n", shortSHA(out.CommitSHA))
	}
	return nil
}

const publishUsage = `usage: cockpit publish [--in FILE]... [--out FILE]... [--outputs-in DIR | --output-dir DIR] [--env-ref REF] [--json] -- COMMAND...
  with an image pinned the cockpit runs COMMAND; without, it records the command you ran`

// shellJoin puts a command back into one line, quoting what the shell would have split.
func shellJoin(words []string) string {
	out := make([]string, len(words))
	for i, w := range words {
		if w == "" || strings.ContainsAny(w, " \t\n'\"\\$`*?[]{}()<>|&;#~!") {
			out[i] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
		} else {
			out[i] = w
		}
	}
	return strings.Join(out, " ")
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
