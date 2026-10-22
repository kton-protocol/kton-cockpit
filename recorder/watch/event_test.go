package watch

import "testing"

// Review S2: v2 escapes a backslash, TAB, LF and CR in path and extra; v1 is read as written.
func TestParse_V2PathsAreUnescaped(t *testing.T) {
	line := "1\t2\t1\topen\tr\t/p/a\\tb\\\\c\t0\t0\t0\tsha256=x"
	if e, _ := ParseEscaped(line); e.Path != "/p/a\tb\\c" {
		t.Fatalf("v2 path %q", e.Path)
	}
	if e, _ := Parse(line); e.Path != `/p/a\tb\\c` {
		t.Fatalf("v1 path %q, want it as written", e.Path)
	}
	for in, want := range map[string]string{`a\nb`: "a\nb", `a\rb`: "a\rb", `a\`: `a\`, `a\xb`: `a\xb`, "plain": "plain"} {
		if got := Unescape(in); got != want {
			t.Errorf("Unescape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSession_AV2HeaderUnescapesItsLines(t *testing.T) {
	s := NewSession("t", "/log.tsv", &Classifier{System: DefaultSystem, Temp: DefaultTemp})
	s.Line("#ktontrace v2\thash=/p")
	s.Line("1\t100\t1\topen\tr\t/p/a\\tb\t3\t0\t0\tsha256=aaa")
	s.Line("2\t100\t1\tTASK\tok\t1\t0\t0\t0\tread.csv(\"a\\tb\")")
	if len(s.Tasks) == 0 || len(s.Tasks[0].Inputs) != 1 || s.Tasks[0].Inputs[0].Path != "/p/a\tb" ||
		s.Tasks[0].Expr != "read.csv(\"a\tb\")" {
		t.Fatalf("tasks %+v", s.Tasks)
	}
}

// Re-check of #12: a v2 shim joined argv by spaces, so a quote there is a character; v3 quotes.
func TestSession_CommandLinesAreReadAsTheirShimWroteThem(t *testing.T) {
	read := func(header, line string) []string {
		s := NewSession("t", "/log.tsv", &Classifier{System: DefaultSystem, Temp: DefaultTemp})
		s.Line(header)
		s.Line("1\t100\t1\texec\t-\t/p\t0\t0\t0\t" + line)
		s.Line("2\t100\t1\tTASK\tok\t1\t0\t0\t0\tx")
		return splitCmdline(s.Tasks[0].Commands[0].Cmdline)
	}
	if got := read("#ktontrace v2\thash=/p", `R -e it's`); len(got) != 3 || got[2] != "it's" {
		t.Fatalf("v2: %q", got)
	}
	if got := read("#ktontrace v3\thash=/p", `nmfe75 'run 1.mod' 'it'\\''s'`); len(got) != 3 || got[1] != "run 1.mod" || got[2] != "it's" {
		t.Fatalf("v3: %q", got)
	}
}
