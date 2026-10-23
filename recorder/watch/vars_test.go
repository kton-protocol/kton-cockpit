package watch

import (
	"sort"
	"strings"
	"testing"
)

func TestLinkVars_FollowsObjectsBetweenCommands(t *testing.T) {
	tasks := []*Task{
		{ID: "1", Expr: `ext <- read.table("model/run1.ext", skip = 1)`, Vars: []Var{{Name: "ext"}}},
		{ID: "2", Expr: `ext$OBJ2 <- ext$OBJ * 2  # a comment mentioning pk`, Vars: []Var{{Name: "ext"}}},
		{ID: "3", Expr: `writeLines(sprintf("OFV %.3f ext pk", min(ext$OBJ2)), "bericht.txt")`},
		{ID: "4", Origins: []Origin{{Code: "pk <- 1\nsummary(pk)"}}, Vars: []Var{{Name: "pk"}}},
		{ID: "5", Expr: "base::invisible(base::.ark_last_value)"},
	}
	LinkVars(tasks)
	if u := tasks[1].Uses; len(u) != 1 || u[0] != (VarUse{"ext", "1"}) {
		t.Fatalf("task 2 %+v", u)
	}
	if u := tasks[2].Uses; len(u) != 1 || u[0] != (VarUse{"ext", "2"}) {
		t.Fatalf("task 3 uses the latest ext, not $OBJ2 or strings: %+v", u)
	}
	if len(tasks[3].Uses) != 0 || len(tasks[4].Uses) != 0 {
		t.Fatalf("no uses expected: %+v %+v", tasks[3].Uses, tasks[4].Uses)
	}
}

// MH's story of gof.png pulled steps 3, 5, 6, 7 and 8 onto the way: 07_simulate.R leaves a variable
// c, and every later c(...) "used" it; 07 itself starts with pop <- read.csv(...) and "used" the
// pop of 05_compare.R. A call is not a use, and a name a script assigns before it reads it is the
// script's own.
func TestUsedNames_CallsAndOwnAssignmentsAreNotUses(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{`x <- c(1, 2)`, ""}, // c is called, x assigned
		{"pop <- read.csv(\"derived/fit_population.csv\")\nest <- pop$estimate", ""},      // pop assigned first
		{"fit <- readRDS(f)\nplot(fit)", "f"},                                             // fit is its own; f is read
		{`sims <- do.call(rbind, lapply(c(100, 200), function(d) read.csv(d)))`, "rbind"}, // d a parameter; rbind passed as a value
		{`f(x = 1, y = pk)`, "pk"},                                                        // x a named argument, pk read
		{"pk$dose_mg <- pk$Dose * 2", "pk"},                                               // changed in place: it must exist
		{"subjects[1, ] <- NA", "subjects"},
		{`read.csv("c.csv") -> d; summary(d)`, ""}, // right assignment
		{"if (n == 3) print(n)", "n"},              // == is a comparison
		{"nca # a comment naming fit", "nca"},
		// 09_gof.R: an apostrophe in a comment opened a "string" that swallowed fit <- readRDS(...)
		{"# from the fit's own data\nfit <- readRDS(\"f.rds\")\npk <- read.csv(\"pk.csv\") # the fit's data names pk\nd <- getData(fit)", ""},
		{"s <- \"a # not a comment\"; print(s, x)", "x"},
	}
	for _, c := range cases {
		var got []string
		for n := range usedNames(c.code) {
			got = append(got, n)
		}
		sort.Strings(got)
		if strings.Join(got, " ") != c.want {
			t.Errorf("%q: used %v, want %q", c.code, got, c.want)
		}
	}
}
