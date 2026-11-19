package cockpit

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// BoundTerm is a typed value a sparql query's variable is bound to (bindTerms): an IRI, or a literal
// with a datatype. It goes into the VALUES clause as an RDF term, checked so that it is one term and
// never query text (SPEC §9.9). A plain literal stays in bind.
type BoundTerm struct {
	// Type is "iri" or "literal".
	Type  string `json:"type" jsonschema:"iri or literal"`
	Value string `json:"value" jsonschema:"the IRI, or the literal's lexical form"`
	// Datatype is, for a literal, xsd:<name> or a full IRI; an xsd type's lexical form is checked.
	Datatype string `json:"datatype,omitempty" jsonschema:"for a literal: xsd:integer, xsd:decimal, xsd:double, xsd:boolean, xsd:date, xsd:dateTime, xsd:string, or a full datatype IRI"`
}

const xsdNS = "http://www.w3.org/2001/XMLSchema#"

// iriTerm is an absolute IRI that can stand between < and > as written: a scheme, and none of the
// characters that would end the term or are not allowed in it (RFC 3987, SPARQL IRIREF).
var iriTerm = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:[^\x00-\x20<>"{}|^` + "`" + `\\]+$`)

// isIRI is iriTerm, plus what the query engine's IRI parser also refuses (kton-review B1 on #161): a
// '%' not followed by two hex digits, DEL, and the Unicode line, paragraph and space separators.
func isIRI(v string) bool {
	if !iriTerm.MatchString(v) || pctBad.MatchString(v) {
		return false
	}
	for _, r := range v {
		if r == 0x7f || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Zs) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// pctBad is a '%' that does not start a percent-encoded octet.
var pctBad = regexp.MustCompile(`%([^0-9A-Fa-f]|[0-9A-Fa-f][^0-9A-Fa-f]|[0-9A-Fa-f]?$)`)

// xsdLexical are the lexical forms checked for the xsd datatypes a binding may name.
var xsdLexical = map[string]*regexp.Regexp{
	"string":   nil,
	"integer":  regexp.MustCompile(`^[+-]?\d+$`),
	"decimal":  regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)$`),
	"double":   regexp.MustCompile(`^([+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?|INF|-INF|NaN)$`),
	"boolean":  regexp.MustCompile(`^(true|false|1|0)$`),
	"date":     regexp.MustCompile(`^-?\d{4,}-\d{2}-\d{2}(Z|[+-]\d{2}:\d{2})?$`),
	"dateTime": regexp.MustCompile(`^-?\d{4,}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?$`),
}

// sparqlTerm writes t as one SPARQL term, or says why it is not one.
func (t BoundTerm) sparqlTerm() (string, error) {
	switch t.Type {
	case "iri":
		if !isIRI(t.Value) {
			return "", fmt.Errorf("%q is not an absolute IRI", t.Value)
		}
		return "<" + t.Value + ">", nil
	case "literal":
		lit := quoteLiteral(t.Value)
		dt := t.Datatype
		switch {
		case dt == "":
			return lit, nil
		case strings.HasPrefix(dt, "xsd:"):
			name := strings.TrimPrefix(dt, "xsd:")
			re, known := xsdLexical[name]
			if !known {
				return "", fmt.Errorf("datatype %s is not one bind checks (%s); name it by its full IRI", dt, strings.Join(xsdNames(), ", "))
			}
			if re != nil && !re.MatchString(t.Value) {
				return "", fmt.Errorf("%q is not an %s", t.Value, dt)
			}
			return lit + "^^<" + xsdNS + name + ">", nil
		case isIRI(dt):
			if name := strings.TrimPrefix(dt, xsdNS); name != dt {
				return BoundTerm{Type: "literal", Value: t.Value, Datatype: "xsd:" + name}.sparqlTerm()
			}
			return lit + "^^<" + dt + ">", nil
		}
		return "", fmt.Errorf("datatype %q is neither xsd:<name> nor an absolute IRI", dt)
	}
	return "", fmt.Errorf("a bound term's type is iri or literal, not %q", t.Type)
}

func xsdNames() []string {
	out := []string{}
	for _, n := range []string{"string", "integer", "decimal", "double", "boolean", "date", "dateTime"} {
		out = append(out, "xsd:"+n)
	}
	return out
}

// quoteLiteral is a value as a quoted SPARQL string, its quote, backslash and line breaks escaped.
func quoteLiteral(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(v) + `"`
}
