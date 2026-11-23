package cockpit

import (
	"fmt"
	"strings"
)

// A query run (ADR-020) must give the same rows wherever and whenever it is evaluated over its pinned
// dataset. checkReproducible refuses what makes an answer depend on the moment or on the order of
// evaluation, decided on the query's words, never inside its strings, IRIs or comments (kton-review A4):
//   - NOW, RAND, UUID, STRUUID, BNODE: a value of the moment (an as-of date is a bound parameter instead);
//   - SAMPLE, GROUP_CONCAT: an answer that depends on the order rows arrive in;
//   - LIMIT or OFFSET, unless at the top level with an ORDER BY that names every projected variable
//     (SELECT * cannot be checked so, and a sub-select's LIMIT is refused): which rows are cut must not
//     depend on evaluation order (kton-review A1). This is stricter than needed, never looser.
func checkReproducible(q string) error {
	words := queryWords(q)
	for i, w := range words {
		switch w.text {
		case "now", "rand", "uuid", "struuid", "bnode":
			if i+1 < len(words) && words[i+1].text == "(" {
				return fmt.Errorf("%s() gives a value of the moment, and a query run must give the same rows again: bind an as-of date as a parameter instead", strings.ToUpper(w.text))
			}
		case "sample", "group_concat":
			if i+1 < len(words) && words[i+1].text == "(" {
				return fmt.Errorf("%s depends on the order rows arrive in, and a query run must give the same rows again", strings.ToUpper(w.text))
			}
		}
	}
	// LIMIT and OFFSET: only at depth 0 (outside every { } group), and then with a total order.
	cut, deep := false, false
	for _, w := range words {
		if w.text == "limit" || w.text == "offset" {
			if w.depth > 0 {
				deep = true
			} else {
				cut = true
			}
		}
	}
	if deep {
		return fmt.Errorf("a LIMIT or OFFSET inside a sub-select cuts rows in an order the run cannot pin")
	}
	if !cut {
		return nil
	}
	projected, star := topProjection(words)
	if star {
		return fmt.Errorf("LIMIT or OFFSET with SELECT *: name the variables, and ORDER BY all of them, so the rows cut are the same every time")
	}
	// With a cut, the ORDER BY may only name variables, bare or in ASC()/DESC(): an expression could tie
	// where its variables do not (kton-review N1 on #225).
	ordered := map[string]bool{}
	inOrder := false
	for _, w := range words {
		if w.depth > 0 {
			continue
		}
		switch {
		case w.text == "order":
			inOrder = true
		case w.text == "limit" || w.text == "offset":
			inOrder = false
		case !inOrder || w.text == "by":
		case strings.HasPrefix(w.text, "?"):
			ordered[w.text] = true
		case w.text == "asc" || w.text == "desc" || w.text == "(" || w.text == ")":
		default:
			return fmt.Errorf("with LIMIT or OFFSET the ORDER BY names variables only (bare, ASC or DESC), not %q: an expression can tie where the variables do not", w.text)
		}
	}
	for _, v := range projected {
		if !ordered[v] {
			return fmt.Errorf("LIMIT or OFFSET without %s in the ORDER BY: the rows cut would depend on evaluation order; order by every projected variable", v)
		}
	}
	return nil
}

type queryWord struct {
	text  string // lower case; a variable keeps its ?, punctuation is one character
	depth int    // { } groups open
}

// queryWords are the words of a query outside its strings, IRIs and comments.
func queryWords(q string) []queryWord {
	var out []queryWord
	depth := 0
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == '#':
			for i < len(q) && q[i] != '\n' && q[i] != '\r' {
				i++
			}
		case c == '<':
			if m := iriRef.FindString(q[i:]); m != "" {
				i += len(m)
			} else {
				out = append(out, queryWord{"<", depth})
				i++
			}
		case c == '"' || c == '\'':
			i = skipString(q, i)
		case c == '{':
			depth++
			i++
		case c == '}':
			depth--
			i++
		case c == '?' || c == '$' || isWordByte(c):
			j := i + 1
			for j < len(q) && isWordByte(q[j]) {
				j++
			}
			w := strings.ToLower(q[i:j])
			if c == '$' {
				w = "?" + w[1:]
			}
			out = append(out, queryWord{w, depth})
			i = j
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			out = append(out, queryWord{string(c), depth})
			i++
		}
	}
	return out
}

func isWordByte(b byte) bool {
	return b == '_' || b == ':' || b == '-' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// topProjection is the top-level SELECT's variables (AS ?x counts as ?x), or star.
func topProjection(words []queryWord) (vars []string, star bool) {
	for i, w := range words {
		if w.depth != 0 || w.text != "select" {
			continue
		}
		for j := i + 1; j < len(words) && words[j].text != "where" && words[j].text != "{"; j++ {
			switch t := words[j].text; {
			case t == "*":
				return nil, true
			case strings.HasPrefix(t, "?"):
				// In (expr AS ?x) only ?x is projected; a variable inside the expression is not.
				if j > 0 && words[j-1].text == "as" {
					vars = append(vars, t)
				} else if parenDepth(words[i+1:j]) == 0 {
					vars = append(vars, t)
				}
			}
		}
		return vars, false
	}
	return nil, false
}

func parenDepth(ws []queryWord) int {
	d := 0
	for _, w := range ws {
		switch w.text {
		case "(":
			d++
		case ")":
			d--
		}
	}
	return d
}
