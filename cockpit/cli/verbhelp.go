package cli

// verbhelp.go answers `cockpit publish|say|ask --help` with the verb's fields. They come from the
// same request types the MCP surface derives its tool schemas from, so a person at a shell reads
// what a session is offered — nothing written twice that could drift.

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

func verbHelp[In any](w io.Writer, verb string) error {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "usage: cockpit %s '<json>' [--field NAME]\n", verb)
	fmt.Fprintf(w, "   or: cockpit %s with words — %s\n\nfields of the JSON form:\n", verb, typedForm[verb])
	writeFields(w, s, "  ")
	if ex, ok := verbExamples[verb]; ok {
		fmt.Fprintf(w, "\nexample:\n%s", ex)
	}
	fmt.Fprintf(w, "\nunknown fields are refused. --field NAME prints one value of the answer.\n")
	return nil
}

func writeFields(w io.Writer, s *jsonschema.Schema, indent string) {
	order := s.PropertyOrder
	if len(order) == 0 {
		for name := range s.Properties {
			order = append(order, name)
		}
		slices.Sort(order)
	}
	for _, name := range order {
		p := s.Properties[name]
		if p == nil {
			continue
		}
		typ := schemaType(p)
		mark := ""
		if slices.Contains(s.Required, name) {
			mark = " (required)"
		}
		fmt.Fprintf(w, "%s%-16s %s%s\n", indent, name, typ, mark)
		if d := strings.TrimSpace(p.Description); d != "" {
			fmt.Fprintf(w, "%s  %s\n", indent, d)
		}
		if obj := objectOf(p); obj != nil && len(obj.Properties) > 0 {
			writeFields(w, obj, indent+"    ")
		}
	}
}

// objectOf is the object schema behind a property: itself, or an array's items.
func objectOf(p *jsonschema.Schema) *jsonschema.Schema {
	if p.Items != nil {
		return p.Items
	}
	return p
}

func schemaType(p *jsonschema.Schema) string {
	t := p.Type
	if t == "" {
		for _, x := range p.Types {
			if x != "null" {
				t = x
			}
		}
	}
	if (t == "array" || slices.Contains(p.Types, "array")) && p.Items != nil {
		return schemaType(p.Items) + "[]"
	}
	if t == "" {
		return "any"
	}
	return t
}

var verbExamples = map[string]string{
	"publish": `  cockpit publish '{"cmd": "python3 clean.py", "inputs": ["data/runs.csv", "clean.py"], "outputs": ["out/clean.csv"]}'
`,
	"say": `  cockpit say '{"template": "working-on", "subject": "sha256:<fotonId>", "fields": {"step": "cleaning", "by-session": "alice"}}'
  cockpit say '{"template": "reproduces", "subject": "sha256:<their fotonId>", "subjectOutputHash": "sha256:<their output>",
                "reproducedOutput": "runs/check/out/clean.csv", "reproducedFotonId": "sha256:<your fotonId>"}'
`,
	"ask": `  cockpit ask '{"query": "record", "ref": "sha256:<fotonId>"}'
  cockpit ask '{"query": "producer", "ref": "out/clean.csv"}'
  cockpit ask '{"query": "reproductions", "ref": "sha256:<output hash>"}'
`,
}

var typedForm = map[string]string{
	"publish": "cockpit publish --in FILE... --out FILE... -- COMMAND",
	"say":     "`cockpit say` lists the templates and their fields",
	"ask":     "`cockpit ask` lists the questions",
}
