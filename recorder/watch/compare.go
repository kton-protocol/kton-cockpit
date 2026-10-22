package watch

import (
	"bufio"
	"bytes"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Comparing two versions of a file that are not the same bytes. A provisional normaliser until
// an environment class brings its own (SPEC §10): text must match, numbers may differ within a
// relative tolerance, and lines that only say when or how long something ran are left out. It
// renders no verdict on whether the tolerance is right; it reports the largest deviation and
// where it is, and the reader decides.

// Comparison is the outcome.
type Comparison struct {
	Verdict string  `json:"verdict"` // same | within | different
	MaxRel  float64 `json:"max_rel"` // largest relative deviation of a number
	Where   string  `json:"where,omitempty"`
	Numbers int     `json:"numbers"`        // numbers compared
	Note    string  `json:"note,omitempty"` // why it is "different"
	Rtol    float64 `json:"rtol"`
}

// volatile: lines that say when or for how long, never what was computed
var volatile = regexp.MustCompile(`(?i)(\b(mon|tue|wed|thu|fri|sat|sun)\b.*\b\d{1,2}:\d{2}(:\d{2})?\b|\belapsed\b|\bcpu ?time\b|\bstart time\b|\bstop time\b|^\s*#CPUT|\b\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}|\blicen[cs]e\b.*expire)`)

var numTok = regexp.MustCompile(`^[-+]?(\d+\.?\d*|\.\d+)([eEdD][-+]?\d+)?$`)

// CompareFiles compares recorded (a) and reproduced (b) bytes.
func CompareFiles(a, b []byte, rtol float64) Comparison {
	c := Comparison{Rtol: rtol}
	if bytes.Equal(a, b) {
		c.Verdict = "same"
		return c
	}
	if binary(a) || binary(b) {
		c.Verdict, c.Note = "different", "Binärdatei (etwa ein Bild): andere Bytes; vergleichen lässt sie sich nur mit einem Normaliser für diese Art Datei"
		return c
	}
	la, lb := lines(a), lines(b)
	if len(la) != len(lb) {
		c.Verdict, c.Note = "different", "andere Zeilenzahl ("+strconv.Itoa(len(lb))+" statt "+strconv.Itoa(len(la))+")"
		for i := 0; i < len(la) && i < len(lb); i++ {
			if la[i] != lb[i] {
				c.Note += "; zuerst anders in Zeile " + strconv.Itoa(i+1) + ": „" + clip(lb[i]) + "“ statt „" + clip(la[i]) + "“"
				break
			}
		}
		return c
	}
	for i := range la {
		if la[i] == lb[i] {
			continue
		}
		if volatile.MatchString(la[i]) && volatile.MatchString(lb[i]) {
			continue
		}
		ta, tb := tokens(la[i]), tokens(lb[i])
		if len(ta) != len(tb) {
			c.Verdict, c.Note = "different", "Zeile "+strconv.Itoa(i+1)+" ist anders aufgebaut"
			return c
		}
		for j := range ta {
			if ta[j] == tb[j] {
				continue
			}
			x, okx := number(ta[j])
			y, oky := number(tb[j])
			if !okx || !oky {
				c.Verdict, c.Note = "different", "Zeile "+strconv.Itoa(i+1)+": „"+ta[j]+"“ statt „"+tb[j]+"“"
				return c
			}
			c.Numbers++
			rel := math.Abs(x-y) / math.Max(math.Max(math.Abs(x), math.Abs(y)), 1e-300)
			if rel > c.MaxRel {
				c.MaxRel = rel
				c.Where = "Zeile " + strconv.Itoa(i+1) + ": " + ta[j] + " statt " + tb[j]
			}
		}
	}
	c.Verdict = "within"
	if c.MaxRel > rtol {
		c.Verdict, c.Note = "different", "Abweichung über der Toleranz"
	}
	return c
}

func lines(b []byte) []string {
	var out []string
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 1<<20), 1<<26)
	for s.Scan() {
		out = append(out, strings.TrimRight(s.Text(), " \r"))
	}
	return out
}

// tokens: a line split at blanks and commas, so CSV and fixed-width tables both work
func tokens(l string) []string {
	return strings.FieldsFunc(l, func(r rune) bool { return r == ' ' || r == '\t' || r == ',' || r == ';' })
}

func number(t string) (float64, bool) {
	t = strings.Trim(t, `"'`)
	if !numTok.MatchString(t) {
		return 0, false
	}
	t = strings.NewReplacer("D", "E", "d", "e").Replace(t)
	f, err := strconv.ParseFloat(t, 64)
	return f, err == nil
}

// binary: a NUL byte in the first 8 KB
func binary(b []byte) bool {
	if len(b) > 8192 {
		b = b[:8192]
	}
	return bytes.IndexByte(b, 0) >= 0
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 70 {
		return string(r[:69]) + "…"
	}
	return s
}
