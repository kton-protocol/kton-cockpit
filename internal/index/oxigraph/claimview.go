package oxigraph

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
	"regexp"
	"sort"
	"strconv"
	"strings"

	ntemplate "kton.dev/nekton/template"
)

// claimview.go builds the query projection of ADR-013 from the kernel's, in view vocabulary 1
// (SPEC §9.11): each claim a trusted key signed becomes one node, <https://kton.dev/o/<hex>>, with its
// predicate, its subjects and fields, its signers, its scope, predecessor and position in the chain,
// a seed's parent, and the replacements claims state (nk:replaces, and nk:replacedBy where ADR-013
// A5 lets one claim retire another). Nothing else is inferred: no claim is "current". It is one
// function of the kernel's quads, so a container started for one query and the daemon build the
// same view.

const (
	nkNS     = "https://kton.dev/v/"
	labNS    = nkNS + "lab/"
	oNS      = "https://kton.dev/o/"
	npNS     = "https://kton.dev/np/"
	attrNS   = "https://kton.dev/attribution/"
	agentNS  = "https://kton.dev/agent/"
	rdfType  = "http://www.w3.org/1999/02/22-rdf-syntax-ns#type"
	rdfsLbl  = "http://www.w3.org/2000/01/rdf-schema#label"
	dctTime  = "http://purl.org/dc/terms/created"
	xsdNS    = "http://www.w3.org/2001/XMLSchema#"
	npAssert = "#assertion"
	npPub    = "#pubinfo"
)

// Quad is one statement of the kernel's projection; G is its graph, "" for the default graph.
type Quad struct {
	G       string
	S, P, O Term
}

func iri(v string) Term               { return Term{Type: "uri", Value: v} }
func lit(v string) Term               { return Term{Type: "literal", Value: v} }
func typed(v, datatype string) Term   { return Term{Type: "literal", Value: v, Datatype: datatype} }
func triple(s, p string, o Term) Quad { return Quad{S: iri(s), P: iri(p), O: o} }
func isClaimGraph(g, suffix string) bool {
	return strings.HasPrefix(g, npNS) && strings.HasSuffix(g, suffix)
}

// claimHex is the hex of a nanopublication's graph or of its content node.
func claimHex(v string) string {
	switch {
	case strings.HasPrefix(v, npNS):
		v = strings.TrimPrefix(v, npNS)
		if i := strings.Index(v, "."); i >= 0 {
			return v[:i]
		}
		return v
	case strings.HasPrefix(v, oNS):
		return strings.TrimPrefix(v, oNS)
	}
	return ""
}

// Signer is what the trust configuration says about a key: the name of its key file and its tier.
type Signer struct{ Name, Tier string }

// ViewConfig is what a configuration adds to the kernel's projection: who its trusted keys are, and
// its templates, whose aliases give a field its term and whose field types give a value its type.
type ViewConfig struct {
	Signers   map[string]Signer // by keyid
	Templates ntemplate.Set
	// Aliases are a package template's own package aliases, by template name: they resolve that
	// template's fields, and the repository's (the set's) only what they leave as it is (SPEC §9.11).
	Aliases map[string]packages.Aliases
	// Rows are the templates whose claims are rows, not versions: each such claim's node says nk:row
	// true, so a "latest per subject" query leaves it out (ADR-018, section 5).
	Rows map[string]bool
}

// resolve is x as the template named tmpl means it: through its package's own aliases when they
// resolve it, else through the repository's. tmpl "" (a claim of no known template) is the repository's.
func (vc ViewConfig) resolve(tmpl, x string) string {
	if r := vc.Aliases[tmpl].Resolve(x); r != x {
		return r
	}
	return vc.Templates.Resolve(x)
}

// viewVersion changes whenever ClaimView builds something else from the same quads: a view kept
// from an older build is then built again rather than answered from.
const viewVersion = 10 // 4: view vocabulary 1; 5: nested fields through the aliases; 6: package aliases; 7: no aliases for an unknown template; 8: scope graphs (ADR-018); 9: nk:row, nk:source, graphs kept; 10: what a claim asserts about something else in no scope graph

// Fingerprint changes whenever the view a ViewConfig builds would.
func (vc ViewConfig) Fingerprint() string {
	type tmpl struct {
		Pred   string
		Fields map[string]string
	}
	var ts []tmpl
	terms := map[string]string{}
	for _, n := range vc.Templates.Names() {
		t, _ := vc.Templates.Get(n)
		f := map[string]string{}
		for k, field := range t.Fields {
			f[k] = field.Type
			terms[n+"#"+k] = vc.resolve(n, k)
		}
		if vc.Rows[n] {
			f["@claims"] = "rows"
		}
		ts = append(ts, tmpl{n + "=" + t.Predicate, f})
	}
	b, _ := json.Marshal(struct {
		V int
		S map[string]Signer
		T []tmpl
		R map[string]string
	}{viewVersion, vc.Signers, ts, terms})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

type claimNode struct {
	hex              string
	pred             Term
	about            []Term   // one or more (kton #254)
	replaces         []string // the hexes its replaces field names, lower case
	parent           Term     // on a seed: the parent scope it names
	fields, other    []Quad
	nested           map[string][]Quad // a field's own node (#o.<field>), such as a ref's hash
	created          Term
	scope, prev      string
	genesis, hasPred bool
	signers          []string
	position         int
}

// ClaimView is the query projection of the claims in trusted (their nanopublication graphs and the
// attribution graphs of trusted keys, as the kernel projects them), in the default graph. structure
// is the publication info of every claim of the union, trusted or not: a chain's positions count
// every claim in it, while only trusted claims are shown and only they replace one another.
func ClaimView(trusted, structure []Quad, vc ViewConfig) []Quad {
	claims := map[string]*claimNode{}
	node := func(h string) *claimNode {
		if claims[h] == nil {
			claims[h] = &claimNode{hex: h, position: -1}
		}
		return claims[h]
	}
	for _, q := range trusted {
		if strings.HasPrefix(q.G, attrNS) {
			rest := strings.TrimPrefix(q.G, attrNS)
			if i := strings.Index(rest, "/"); i > 0 {
				c := node(rest[:i])
				if kid := rest[i+1:]; !contains(c.signers, kid) {
					c.signers = append(c.signers, kid)
				}
			}
		}
	}
	for _, q := range trusted {
		h := claimHex(q.G)
		c := claims[h]
		if c == nil || len(c.signers) == 0 {
			continue // no trusted key verifies it
		}
		switch {
		case isClaimGraph(q.G, npAssert):
			helper := npNS + h + ".#o"
			switch {
			case strings.HasPrefix(q.S.Value, helper+"."):
				if c.nested == nil {
					c.nested = map[string][]Quad{}
				}
				c.nested[q.S.Value] = append(c.nested[q.S.Value], q)
			case q.S.Value == helper:
				c.fields = append(c.fields, q)
			case q.O.Value == helper:
				if !containsTerm(c.about, q.S) {
					c.about = append(c.about, q.S)
				}
				c.pred, c.hasPred = q.P, true
			default:
				c.other = append(c.other, q)
			}
		case isClaimGraph(q.G, npPub):
			switch q.P.Value {
			case dctTime:
				c.created = q.O
			case nkNS + "genesis":
				c.genesis = q.O.Value == "true"
			}
		}
	}

	// The chains, from every claim of the union.
	links := map[string][2]string{} // hex -> scope, prev
	for _, q := range structure {
		h := claimHex(q.S.Value)
		if h == "" || !isClaimGraph(q.G, npPub) {
			continue
		}
		l := links[h]
		switch q.P.Value {
		case nkNS + "scope":
			l[0] = claimHex(q.O.Value)
		case nkNS + "prev":
			l[1] = claimHex(q.O.Value)
		}
		links[h] = l
	}
	positions := map[string]int{}
	var pos func(h string, depth int) int
	pos = func(h string, depth int) int {
		if p, ok := positions[h]; ok {
			return p
		}
		l, ok := links[h]
		switch {
		case !ok || depth > len(links):
			return -1
		case l[0] == h:
			positions[h] = 0 // a seed
			return 0
		case l[1] == "":
			return -1
		}
		p := pos(l[1], depth+1)
		if p >= 0 {
			p++
		}
		positions[h] = p
		return p
	}
	for h, c := range claims {
		l := links[h]
		c.scope, c.prev = l[0], l[1]
		c.position = pos(h, 0)
		for _, f := range c.fields {
			if f.P.Value == nkNS+"replaces" {
				if x := strings.ToLower(claimHex(f.O.Value)); x != "" && !contains(c.replaces, x) {
					c.replaces = append(c.replaces, x)
				}
			}
		}
		for _, q := range c.other {
			if c.genesis && q.P.Value == nkNS+"parent" {
				c.parent = q.O
			}
		}
	}

	// Replaced (ADR-013 A5): a claim names what it replaces in its replaces field, and every such
	// name is projected as nk:replaces whoever signed either claim. A replacement retires a claim
	// (nk:replacedBy) only when every trusted signer of the replaced claim signed the replacement too,
	// it has the same template, and the two share a subject: one of two co-signers cannot withdraw
	// the other's signature (kton-review C1 on #146). What any other name means is a rule's decision.
	replacedBy := map[string][]string{}
	for _, r := range claims {
		if len(r.signers) == 0 || !r.hasPred {
			continue
		}
		for _, x := range r.replaces {
			old := claims[x]
			if old == nil || old == r || len(old.signers) == 0 || !old.hasPred || old.pred.Value != r.pred.Value ||
				!subsetOf(old.signers, r.signers) || !sharesTerm(old.about, r.about) {
				continue
			}
			if !contains(replacedBy[x], r.hex) {
				replacedBy[x] = append(replacedBy[x], r.hex)
			}
		}
	}

	byPred := map[string]ntemplate.Template{}
	for _, n := range vc.Templates.Names() {
		if t, ok := vc.Templates.Get(n); ok && t.Predicate != "" {
			byPred[vc.Templates.Resolve(t.Predicate)] = t
		}
	}

	var out []Quad
	var shared []int // indexes of quads about something other than the claim
	agents := map[string]bool{}
	hexes := make([]string, 0, len(claims))
	for h := range claims {
		hexes = append(hexes, h)
	}
	sort.Strings(hexes)
	for _, h := range hexes {
		c := claims[h]
		if len(c.signers) == 0 {
			continue
		}
		n := oNS + h
		start := len(out)
		out = append(out, triple(n, rdfType, iri(nkNS+"Claim")))
		if c.hasPred {
			t, known := byPred[c.pred.Value]
			out = append(out, triple(n, nkNS+"predicate", c.pred))
			for _, a := range c.about {
				out = append(out, Quad{S: iri(n), P: iri(nkNS + "about"), O: a})
			}
			if known {
				out = append(out, triple(n, nkNS+"template", lit(t.Name)))
				if vc.Rows[t.Name] {
					out = append(out, triple(n, nkNS+"row", typed("true", xsdNS+"boolean")))
				}
			}
			tmpl := ""
			if known {
				tmpl = t.Name
			}
			// A claim whose template this repository does not know (one a federation source signed) has
			// no aliases here: its fields stay lab:<field>. The repository's alias file is the vocabulary
			// of the templates it resolves, never another source's (SPEC §9.11; userinterface, 20.11.).
			resolve := func(x string) string {
				if !known {
					return x
				}
				return vc.resolve(tmpl, x)
			}
			for _, f := range c.fields {
				p, o := f.P, f.O
				if key := strings.TrimPrefix(p.Value, labNS); key != p.Value {
					p = aliased(p, resolve)
					if known {
						o = typedValue(o, t.Fields[key].Type, resolve)
					}
				}
				// A field with a node of its own (a ref: the file or step it names, under lab:hash)
				// points at what it names, not at a node inside the nanopublication.
				if inner, ok := c.nested[o.Value]; ok {
					if hash := values(inner, o.Value, labNS+"hash"); len(hash) == 1 {
						o = iri(hash[0])
					} else {
						// A nested object's fields (a list of objects, such as a qualification's
						// role and level) go through the aliases as a top-level field's do, at
						// any depth.
						out = append(out, nestedFields(c.nested, o.Value, resolve, map[string]bool{})...)
					}
				}
				if p.Value == nkNS+"replaces" {
					// A name in lower case, so it is the node of the claim it names (A5, M3).
					if x := claimHex(o.Value); x != "" {
						o = iri(oNS + strings.ToLower(x))
					}
				}
				out = append(out, Quad{S: iri(n), P: p, O: o})
			}
			later := replacedBy[h]
			sort.Strings(later)
			for _, r := range later {
				out = append(out, triple(n, nkNS+"replacedBy", iri(oNS+r)))
			}
		}
		for _, q := range c.other {
			// What a claim asserts about something else (a seed's name for its scope) can be asserted by
			// claims of several scopes alike: it belongs to no scope's graph, so the graphs hold only their
			// own claims' triples and never share one (kton-review N1 on #226).
			shared = append(shared, len(out))
			out = append(out, Quad{S: q.S, P: q.P, O: q.O})
			// A seed's assertion names its scope; the seed node carries that name too, so a scope
			// is found by its name without joining through the assertion.
			if c.genesis && q.P.Value == rdfsLbl {
				out = append(out, Quad{S: iri(n), P: q.P, O: q.O})
			}
		}
		if c.genesis {
			out = append(out, triple(n, nkNS+"genesis", typed("true", xsdNS+"boolean")))
			// The parent scope a seed names (kton #241), on the seed node: the hierarchy is walked
			// from seed to seed without joining through the assertion.
			if c.parent.Value != "" {
				parent := c.parent
				if x := claimHex(parent.Value); x != "" {
					parent = iri(oNS + strings.ToLower(x))
				}
				out = append(out, triple(n, nkNS+"parent", parent))
			}
		}
		if c.created.Value != "" {
			out = append(out, Quad{S: iri(n), P: iri(dctTime), O: c.created})
		}
		if c.scope != "" {
			out = append(out, triple(n, nkNS+"scope", iri(oNS+c.scope)))
		}
		if c.prev != "" {
			out = append(out, triple(n, nkNS+"prev", iri(oNS+c.prev)))
		}
		if c.position >= 0 {
			out = append(out, triple(n, nkNS+"position", typed(strconv.Itoa(c.position), xsdNS+"integer")))
		}
		sort.Strings(c.signers)
		for _, kid := range c.signers {
			out = append(out, triple(n, nkNS+"signer", iri(agentNS+kid)))
			agents[kid] = true
		}
		// The graph this claim belongs to (ADR-018): its signed scope; a seed's, its own; none, the
		// unscoped graph. Signer labels below belong to no scope and stay in the union only.
		g := c.scope
		if g == "" && c.genesis {
			g = h
		}
		if g == "" {
			g = Unscoped
		}
		for i := start; i < len(out); i++ {
			out[i].G = g
		}
		for _, i := range shared {
			out[i].G = ""
		}
		shared = shared[:0]
	}
	kids := make([]string, 0, len(agents))
	for kid := range agents {
		kids = append(kids, kid)
	}
	sort.Strings(kids)
	for _, kid := range kids {
		if s, ok := vc.Signers[kid]; ok {
			out = append(out, triple(agentNS+kid, rdfsLbl, lit(s.Name)), triple(agentNS+kid, nkNS+"tier", lit(s.Tier)))
		}
	}
	return out
}

var (
	isInteger  = regexp.MustCompile(`^[+-]?\d+$`)
	isDate     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	isIRI      = regexp.MustCompile(`^(https?://|urn:|tag:|did:|mailto:)\S+$`)
	isDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})$`)
)

// typedValue gives a plain literal the type its template field declares. A value that does not
// read as that type stays as it was signed. An enum value written as an IRI, or as a CURIE the alias
// file resolves to one, is that IRI, so a rule can match it against a vocabulary's terms; a bare
// value ("approve") stays a literal.
func typedValue(o Term, fieldType string, resolve func(string) string) Term {
	if o.Type != "literal" || o.Datatype != "" || o.Lang != "" {
		return o
	}
	switch fieldType {
	case "date":
		if isDate.MatchString(o.Value) {
			return typed(o.Value, xsdNS+"date")
		}
		if isDateTime.MatchString(o.Value) {
			return typed(o.Value, xsdNS+"dateTime")
		}
	case "integer":
		if isInteger.MatchString(o.Value) {
			return typed(o.Value, xsdNS+"integer")
		}
	case "enum":
		if strings.Contains(o.Value, ":") {
			if r := resolve(o.Value); strings.Contains(r, "://") && !strings.ContainsAny(r, " <>\"{}|^`\\") && !Reserved(r) {
				return iri(r)
			}
		}
	case "ref":
		if strings.HasPrefix(o.Value, "sha256:") {
			return iri(oNS + strings.ToLower(strings.TrimPrefix(o.Value, "sha256:")))
		}
	case "iri":
		// The kernel makes a value with :// an IRI already; urn:, tag:, did: and mailto: values, and a
		// CURIE the alias file resolves, stay literals there and become IRIs here.
		v := o.Value
		if r := resolve(v); strings.Contains(r, "://") {
			v = r
		}
		if isIRI.MatchString(v) && !strings.ContainsAny(v, " <>\"{}|^`\\") && !Reserved(v) {
			return iri(v)
		}
	}
	return o
}

// nestedFields are the quads of the nested node s and of the nodes below it, each field through the
// aliases; seen keeps a node from being emitted twice.
func nestedFields(nested map[string][]Quad, s string, resolve func(string) string, seen map[string]bool) []Quad {
	if seen[s] {
		return nil
	}
	seen[s] = true
	var out []Quad
	for _, q := range nested[s] {
		out = append(out, Quad{S: q.S, P: aliased(q.P, resolve), O: q.O})
		if _, deeper := nested[q.O.Value]; deeper {
			out = append(out, nestedFields(nested, q.O.Value, resolve, seen)...)
		}
	}
	return out
}

// aliased is a field's term: lab:<field> as the aliases resolve it, when they resolve it to an IRI
// that is not one the view writes itself; otherwise lab:<field> as it is.
func aliased(p Term, resolve func(string) string) Term {
	key := strings.TrimPrefix(p.Value, labNS)
	if key == p.Value {
		return p
	}
	if r := resolve(key); strings.Contains(r, "://") && !Reserved(r) {
		return iri(r)
	}
	return p
}

// values are the objects of s p among qs.
func values(qs []Quad, s, p string) []string {
	var out []string
	for _, q := range qs {
		if q.S.Value == s && q.P.Value == p {
			out = append(out, q.O.Value)
		}
	}
	return out
}

func containsTerm(ts []Term, t Term) bool {
	for _, u := range ts {
		if u.Type == t.Type && u.Value == t.Value {
			return true
		}
	}
	return false
}

func sharesTerm(a, b []Term) bool {
	for _, t := range a {
		if containsTerm(b, t) {
			return true
		}
	}
	return false
}

// subsetOf reports whether every element of a is in b.
func subsetOf(a, b []string) bool {
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ParseNQuads reads the kernel's canonical N-Quads (and N-Triples): one statement per line.
func ParseNQuads(b []byte) ([]Quad, error) {
	var out []Quad
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var terms []Term
		rest := strings.TrimSuffix(strings.TrimSpace(line), ".")
		for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
			t, r, err := readTerm(rest)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			terms, rest = append(terms, t), r
		}
		q := Quad{}
		switch len(terms) {
		case 4:
			q.G = terms[3].Value
			fallthrough
		case 3:
			q.S, q.P, q.O = terms[0], terms[1], terms[2]
		default:
			return nil, fmt.Errorf("line %d: %d terms", n, len(terms))
		}
		out = append(out, q)
	}
	return out, sc.Err()
}

func readTerm(s string) (Term, string, error) {
	switch {
	case strings.HasPrefix(s, "<"):
		i := strings.Index(s, ">")
		if i < 0 {
			return Term{}, "", fmt.Errorf("unclosed IRI")
		}
		return iri(s[1:i]), s[i+1:], nil
	case strings.HasPrefix(s, "_:"):
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			i = len(s)
		}
		return Term{Type: "bnode", Value: s[2:i]}, s[i:], nil
	case strings.HasPrefix(s, `"`):
		i := 1
		for ; i < len(s); i++ {
			if s[i] == '\\' {
				i++
			} else if s[i] == '"' {
				break
			}
		}
		if i >= len(s) {
			return Term{}, "", fmt.Errorf("unclosed literal")
		}
		v, err := strconv.Unquote(s[:i+1])
		if err != nil {
			return Term{}, "", fmt.Errorf("literal %s: %w", s[:i+1], err)
		}
		t, rest := lit(v), s[i+1:]
		switch {
		case strings.HasPrefix(rest, "^^<"):
			j := strings.Index(rest, ">")
			t.Datatype, rest = rest[3:j], rest[j+1:]
		case strings.HasPrefix(rest, "@"):
			j := strings.IndexAny(rest, " \t")
			if j < 0 {
				j = len(rest)
			}
			t.Lang, rest = rest[1:j], rest[j:]
		}
		if t.Datatype == xsdNS+"string" {
			t.Datatype = ""
		}
		return t, rest, nil
	}
	return Term{}, "", fmt.Errorf("unexpected %q", s[:min(len(s), 20)])
}

// NTriples writes quads as N-Triples, graphs dropped.
func NTriples(qs []Quad) []byte {
	var b strings.Builder
	for _, q := range qs {
		b.WriteString(ntTerm(q.S) + " " + ntTerm(q.P) + " " + ntTerm(q.O) + " .\n")
	}
	return []byte(b.String())
}

func ntTerm(t Term) string {
	switch t.Type {
	case "uri":
		return "<" + t.Value + ">"
	case "bnode":
		return "_:" + t.Value
	}
	s := ntQuote(t.Value)
	switch {
	case t.Lang != "":
		s += "@" + t.Lang
	case t.Datatype != "":
		s += "^^<" + t.Datatype + ">"
	}
	return s
}

// ntQuote is an N-Triples string literal: only the escapes the grammar has (ECHAR, UCHAR).
func ntQuote(v string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range v {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Reserved tells whether a term is one the query projection writes itself (nk:signer, nk:replacedBy,
// nk:position, …): a field aliased to one would put a second value beside the computed one.
func Reserved(term string) bool {
	return strings.HasPrefix(term, nkNS) && !strings.HasPrefix(term, labNS)
}
