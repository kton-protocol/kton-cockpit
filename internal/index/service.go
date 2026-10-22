package index

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ErrService refuses a query that calls another SPARQL endpoint. One daemon holds every repository's
// views, and a query is confined to its own view only by the dataset the protocol request names. A
// SERVICE clause leaves that dataset: pointed at the daemon itself, it reads every other view,
// with attribution made under keys this configuration does not trust (SPEC §12.4), and pointed
// anywhere else it sends the query out. No vocabulary query needs it.
var ErrService = fmt.Errorf("the query calls another endpoint (SERVICE), which the cockpit does not run")

// CheckQuery refuses a query that uses SERVICE. It reads the query the way a SPARQL parser
// tokenizes it (the SPARQL 1.1 grammar, section 19), so a keyword is found where the grammar has
// one and nowhere else: not inside a string, an IRI or a comment, not as a variable (?service) or
// part of a prefixed name (ex:SERVICE), in any case and after codepoint escapes (\u0053ERVICE) are
// applied. A query it cannot read to the end (an unterminated string, a malformed escape) is
// refused too, since what it would mean cannot be checked.
func CheckQuery(q string) error {
	q, err := unescapeCodepoints(q)
	if err != nil {
		return err
	}
	r := []rune(q)
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case c == '#':
			for i < len(r) && r[i] != '\n' && r[i] != '\r' {
				i++
			}
		case c == '"' || c == '\'':
			n, err := skipString(r, i)
			if err != nil {
				return err
			}
			i = n
		case c == '<':
			i = skipIRI(r, i) // or the operator <, which skipIRI leaves at one rune
		case c == '?' || c == '$':
			i++
			for i < len(r) && nameRune(r[i]) {
				i++
			}
		case nameRune(c) || c == ':':
			start := i
			for i < len(r) && (nameRune(r[i]) || r[i] == ':' || r[i] == '.' || r[i] == '%' || r[i] == '\\') {
				if r[i] == '\\' && i+1 < len(r) {
					i++ // PN_LOCAL_ESC
				}
				i++
			}
			for i > start+1 && r[i-1] == '.' { // a name never ends in a dot; that dot ends the triple
				i--
			}
			word := string(r[start:i])
			if !strings.Contains(word, ":") && strings.EqualFold(word, "SERVICE") {
				return ErrService
			}
		default:
			i++
		}
	}
	return nil
}

func nameRune(c rune) bool {
	return c == '_' || c == '-' || unicode.IsLetter(c) || unicode.IsDigit(c) || c == '·' ||
		(c >= '̀' && c <= 'ͯ') || (c >= '‿' && c <= '⁀')
}

// skipString returns the index after the string literal starting at i, short or long.
func skipString(r []rune, i int) (int, error) {
	q := r[i]
	if i+2 < len(r) && r[i+1] == q && r[i+2] == q {
		for j := i + 3; j < len(r); j++ {
			if r[j] == '\\' {
				j++
				continue
			}
			if j+2 < len(r) && r[j] == q && r[j+1] == q && r[j+2] == q {
				for j+3 < len(r) && r[j+3] == q { // """a"""" ends with the last three
					j++
				}
				return j + 3, nil
			}
		}
		return 0, fmt.Errorf("the query has an unterminated string")
	}
	for j := i + 1; j < len(r); j++ {
		switch r[j] {
		case '\\':
			j++
		case q:
			return j + 1, nil
		case '\n', '\r':
			return 0, fmt.Errorf("the query has an unterminated string")
		}
	}
	return 0, fmt.Errorf("the query has an unterminated string")
}

// skipIRI returns the index after the IRIREF starting at i, or i+1 when '<' is not one (the
// less-than operator).
func skipIRI(r []rune, i int) int {
	for j := i + 1; j < len(r); j++ {
		c := r[j]
		if c == '>' {
			return j + 1
		}
		if c <= ' ' || strings.ContainsRune("<\"{}|^`\\", c) {
			return i + 1
		}
	}
	return i + 1
}

// unescapeCodepoints applies \uXXXX and \UXXXXXXXX everywhere in the query, which SPARQL does before
// parsing (SPARQL 1.1, section 19.2).
func unescapeCodepoints(q string) (string, error) {
	if !strings.Contains(q, `\u`) && !strings.Contains(q, `\U`) {
		return q, nil
	}
	var b strings.Builder
	for i := 0; i < len(q); i++ {
		if q[i] == '\\' && i+1 < len(q) && (q[i+1] == 'u' || q[i+1] == 'U') {
			n := 4
			if q[i+1] == 'U' {
				n = 8
			}
			if i+2+n > len(q) {
				return "", fmt.Errorf("the query has a short codepoint escape")
			}
			v, err := strconv.ParseUint(q[i+2:i+2+n], 16, 32)
			if err != nil {
				return "", fmt.Errorf("the query has a malformed codepoint escape")
			}
			b.WriteRune(rune(v))
			i += 1 + n
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String(), nil
}
