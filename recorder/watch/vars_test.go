package watch

import "testing"

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
