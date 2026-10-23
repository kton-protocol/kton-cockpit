package index

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ErrService refuses a query that calls another SPARQL endpoint. One daemon holds every repository's
// records, and a query is confined to its own union only by the dataset the protocol request names. A
// SERVICE clause leaves that dataset: pointed at the daemon itself, it reads every other repository's,
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
	return keywords(q, func(word string) error {
		if strings.EqualFold(word, "SERVICE") {
			return ErrService
		}
		return nil
	})
}

// ErrFrom refuses a query that names its own dataset. The dataset is the cockpit's to choose
// (SPEC §12.4): the daemon overrides FROM with the one of the request, a container for one query would
// honour it, so the same query would answer differently by where it ran.
var ErrFrom = fmt.Errorf("the query names its own dataset (FROM), which the cockpit chooses: leave FROM out, and use GRAPH for the claims")

// CheckFreeQuery checks a query someone wrote rather than one an allowed package brings (ask's
// sparql): what CheckQuery refuses, a FROM clause, and anything but a SELECT or an ASK. It returns
// the query form, "SELECT" or "ASK".
func CheckFreeQuery(q string) (string, error) {
	form := ""
	err := keywords(q, func(word string) error {
		w := strings.ToUpper(word)
		switch {
		case w == "SERVICE":
			return ErrService
		case w == "FROM":
			return ErrFrom
		case form != "":
		case w == "BASE" || w == "PREFIX":
		case w == "SELECT" || w == "ASK":
			form = w
		default:
			return fmt.Errorf("the query is a %s; only SELECT and ASK are answered", w)
		}
		return nil
	})
	if err == nil && form == "" {
		err = fmt.Errorf("the query is neither a SELECT nor an ASK")
	}
	return form, err
}

// keywords calls fn with every word of the query the grammar could read as a keyword, in order.
func keywords(q string, fn func(word string) error) error {
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
			if word := string(r[start:i]); !strings.Contains(word, ":") {
				if err := fn(word); err != nil {
					return err
				}
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
