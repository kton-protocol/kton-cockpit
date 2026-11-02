package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/kton-protocol/kton-cockpit/cockpit"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/mcpsurface"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpOptions is how the operator starts the MCP server (`cockpit mcp …`, in .mcp.json). What it
// fixes, it fixes for the server's lifetime: a session using the tools cannot choose another
// identity, scope or binding, exactly as it cannot choose another template ceiling.
type mcpOptions struct {
	// vocabulary adds one tool per query and per template of the allowed packages (SPEC §6.2).
	vocabulary bool
	// as is the identity every say over this server signs as (SPEC §8.7); a call naming another is
	// refused, so a session cannot sign as the repository's own key or as another lane.
	as string
	// scope is the configured claim scope the vocabulary's templates chain into, and the subject
	// they are about unless a call names one.
	scope string
	// bind gives query parameters fixed values; a bound parameter is not in the tool's schema.
	bind map[string]string
}

func parseMCPArgs(args []string) (mcpOptions, error) {
	o := mcpOptions{bind: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("mcp: %s needs a value", a)
			}
			i++
			return args[i], nil
		}
		var err error
		switch a {
		case "--vocabulary":
			o.vocabulary = true
		case "--as":
			o.as, err = next()
		case "--scope":
			o.scope, err = next()
		case "--bind":
			var kv string
			if kv, err = next(); err == nil {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || !paramName.MatchString(k) {
					return o, fmt.Errorf("mcp: --bind takes VAR=VALUE, not %q", kv)
				}
				o.bind[k] = v
			}
		default:
			return o, fmt.Errorf("mcp: unknown argument %q (see cockpit mcp --help)", a)
		}
		if err != nil {
			return o, err
		}
	}
	if !o.vocabulary && (o.scope != "" || len(o.bind) > 0) {
		return o, fmt.Errorf("mcp: --scope and --bind fix the vocabulary tools; give --vocabulary too")
	}
	return o, nil
}

// sayAs pins the server's identity onto a say request: with --as, every claim is signed as that
// name, and a request naming another is refused rather than quietly re-signed.
func (o mcpOptions) sayAs(in cockpit.SayRequest) (cockpit.SayRequest, error) {
	if o.as == "" {
		return in, nil
	}
	if in.As != "" && in.As != o.as {
		return in, fmt.Errorf("cockpit: refused (identity.as, SPEC §6.2): this server signs as %q, not %q", o.as, in.As)
	}
	in.As = o.as
	return in, nil
}

// sparqlParam is a query parameter: `$NAME`, the packages' convention for what a caller supplies
// (`?NAME` is a variable the query binds itself).
var (
	sparqlParam = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
	paramName   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// queryParams are the parameters a query takes, in order, and its leading comment as a description.
func queryParams(q string) (params []string, about string) {
	seen := map[string]bool{}
	var lead []string
	inLead := true
	for _, l := range strings.Split(q, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			if inLead {
				lead = append(lead, strings.TrimSpace(strings.TrimPrefix(t, "#")))
			}
			continue
		}
		if t != "" && !strings.HasPrefix(strings.ToUpper(t), "PREFIX") {
			inLead = false
		}
		if i := strings.Index(l, " #"); i >= 0 {
			l = l[:i] // a trailing comment; IRIs keep their # since they carry no space before it
		}
		for _, m := range sparqlParam.FindAllStringSubmatch(l, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				params = append(params, m[1])
			}
		}
	}
	// The first paragraph of the leading comment says what the query answers.
	var para []string
	for _, l := range lead {
		if l == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		para = append(para, l)
	}
	return params, strings.Join(para, " ")
}

// toolName is name as an MCP tool name: letters, digits and underscores.
func toolName(prefix, name string) string {
	return prefix + "_" + regexp.MustCompile(`[^A-Za-z0-9_]`).ReplaceAllString(name, "_")
}

func objectSchema(props map[string]*jsonschema.Schema, required []string) *jsonschema.Schema {
	sort.Strings(required)
	return &jsonschema.Schema{Type: "object", Properties: props, Required: required,
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}
}

// vocabTool is one tool the allowed packages bring: a query (ask sparql --name) or a template
// (say), with what the operator fixed already applied.
type vocabTool struct {
	tool *mcp.Tool
	call func(ctx context.Context, args map[string]any) (any, error)
}

// vocabularyTools are the tools of the allowed packages, read once when the server starts: a
// package allowed later needs a restart, the same as a new .mcp.json entry.
func vocabularyTools(cfg *config.Config, c *cockpit.Cockpit, o mcpOptions) ([]vocabTool, error) {
	voc, err := packages.Allowed(cfg.RepoRoot, cfg.Raw.Claims.AllowedPackages)
	if err != nil {
		return nil, err
	}
	subject := ""
	if o.scope != "" {
		id, ok := cfg.Raw.Claims.Scopes[o.scope]
		if !ok {
			return nil, fmt.Errorf("mcp: --scope %q is not one of claims.scopes", o.scope)
		}
		subject = id
	}
	var out []vocabTool
	used := map[string]bool{}

	names := make([]string, 0, len(voc.Queries))
	for n := range voc.Queries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		params, about := queryParams(string(voc.Queries[name]))
		props := map[string]*jsonschema.Schema{
			"limit": {Type: "integer", Description: "the most rows to return (default 1000)"},
		}
		var required, fixed []string
		bind := map[string]string{}
		for _, p := range params {
			if v, ok := o.bind[p]; ok {
				bind[p] = v
				fixed = append(fixed, p+"="+v)
				continue
			}
			props[p] = &jsonschema.Schema{Type: "string", Description: "binds $" + p + " (a plain literal)"}
			required = append(required, p)
		}
		src := voc.From["query:"+name]
		desc := strings.TrimSpace(about + fmt.Sprintf(" — query %s of the package %s, read like ask sparql --name %s: "+
			"only what a key in this repository's trust tiers signed is in it.", name, src.Name, name))
		if len(fixed) > 0 {
			desc += " Fixed by this server: " + strings.Join(fixed, ", ") + "."
		}
		tn := toolName("ask", name)
		used[tn] = true
		qname := name
		out = append(out, vocabTool{
			tool: &mcp.Tool{Name: tn, Description: desc, InputSchema: objectSchema(props, required)},
			call: func(ctx context.Context, args map[string]any) (any, error) {
				in := cockpit.AskRequest{Query: "sparql", Name: qname, Bind: map[string]string{}}
				for k, v := range bind {
					in.Bind[k] = v
				}
				for k, v := range args {
					if k == "limit" {
						f, _ := v.(float64)
						in.Limit = int(f)
						continue
					}
					in.Bind[k] = fmt.Sprint(v)
				}
				res, err := c.Ask(ctx, in)
				if err != nil {
					return nil, err
				}
				return res.Sparql, nil
			},
		})
	}

	names = names[:0]
	for n := range voc.Templates {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		var t templateInfo
		if err := json.Unmarshal(voc.Templates[name], &t); err != nil {
			return nil, fmt.Errorf("the template %s: %v", name, err)
		}
		if name == "reproduces" {
			continue // its level is computed from a reproduction, not typed; cockpit_say takes it
		}
		props := map[string]*jsonschema.Schema{}
		var required []string
		for f, spec := range t.Fields {
			if f == "subject" {
				return nil, fmt.Errorf("the template %s has a field named subject, which its tool needs for what the claim is about", name)
			}
			s := &jsonschema.Schema{Type: "string", Description: spec.Type}
			for _, v := range spec.Values {
				s.Enum = append(s.Enum, v)
			}
			props[f] = s
			if spec.Required {
				required = append(required, f)
			}
		}
		props["subject"] = &jsonschema.Schema{Type: "string", Description: "what the claim is about: a sha256 record id or a hash"}
		desc := fmt.Sprintf("Say %s (the template %s of the package %s): a signed claim, checked against the trust tiers when read.",
			t.Predicate, name, voc.From["template:"+name].Name)
		if subject != "" {
			desc += fmt.Sprintf(" It is about the scope %s unless subject names something else, and chained into that scope.", o.scope)
		} else {
			required = append(required, "subject")
		}
		if o.as != "" {
			desc += fmt.Sprintf(" Signed as %s.", o.as)
		}
		tn := toolName("say", name)
		if used[tn] {
			return nil, fmt.Errorf("two vocabulary tools would be named %s", tn)
		}
		used[tn] = true
		tname := name
		out = append(out, vocabTool{
			tool: &mcp.Tool{Name: tn, Description: desc, InputSchema: objectSchema(props, required)},
			call: func(ctx context.Context, args map[string]any) (any, error) {
				in := cockpit.SayRequest{Template: tname, Subject: subject, Scope: o.scope, Fields: map[string]string{}}
				for k, v := range args {
					if k == "subject" {
						in.Subject = fmt.Sprint(v)
						continue
					}
					in.Fields[k] = fmt.Sprint(v)
				}
				in, err := o.sayAs(in)
				if err != nil {
					return nil, err
				}
				return c.Say(ctx, in)
			},
		})
	}
	return out, nil
}

// addVocabulary registers the vocabulary tools on server.
func addVocabulary(server *mcp.Server, tools []vocabTool) {
	for _, vt := range tools {
		call := vt.call
		mcp.AddTool(server, vt.tool, mcpsurface.Handler(func(ctx context.Context, args map[string]any) (*any, error) {
			v, err := call(ctx, args)
			if err != nil {
				return nil, err
			}
			return &v, nil
		}))
	}
}
