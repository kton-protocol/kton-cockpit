package watch

import (
	"os"
	"regexp"
	"strings"
)

// VarUse says a task used a variable that an earlier task of the same session left behind.
// Data that flows through R objects instead of files is otherwise invisible to the shim:
// read.csv in one command, three commands of reshaping, write.csv at the end.
type VarUse struct {
	Name string `json:"name"`
	Task string `json:"task"` // the task that last defined it before this one
}

var (
	rString  = regexp.MustCompile(`"(\\.|[^"\\])*"|'(\\.|[^'\\])*'|` + "`[^`]*`")
	rComment = regexp.MustCompile(`#[^\n]*`)
	rIdent   = regexp.MustCompile(`[A-Za-z.][A-Za-z0-9._]*`)
)

// Code is the code a task ran as far as it is known: what Positron sent, else the expression
// R saw (not Ark's own `.ark_last_value`).
// Origin is the command that names the task: the last one sent with a file (a script, a chunk),
// else the last one. Earlier origins of the same task are commands that ran before it without a
// task boundary in between; when the task began is the first origin's business (commandStart).
func (t *Task) Origin() *Origin {
	for i := len(t.Origins) - 1; i >= 0; i-- {
		if t.Origins[i].File != "" {
			return &t.Origins[i]
		}
	}
	if len(t.Origins) > 0 {
		return &t.Origins[len(t.Origins)-1]
	}
	return nil
}

func (t *Task) Code() string {
	// every command the task saw, in the order they were sent: a command that did not close a task
	// of its own (getwd() typed before a source()) ran inside this one
	var codes []string
	for _, o := range t.Origins {
		if o.Code != "" {
			codes = append(codes, strings.TrimRight(o.Code, "\n"))
		}
	}
	if len(codes) > 0 {
		return strings.Join(codes, "\n")
	}
	if strings.HasPrefix(t.Expr, "base::") {
		return ""
	}
	return t.Expr
}

// identifiers in R code, without strings, comments, `$col`, `@slot` and `pkg::fun`
func identifiers(code string) map[string]bool {
	code = rString.ReplaceAllString(code, `""`)
	code = rComment.ReplaceAllString(code, "")
	out := map[string]bool{}
	for _, m := range rIdent.FindAllStringIndex(code, -1) {
		if m[0] > 0 {
			if p := code[m[0]-1]; p == '$' || p == '@' || p == ':' {
				continue
			}
		}
		out[code[m[0]:m[1]]] = true
	}
	return out
}

// LinkVars fills Uses for the tasks of one session, in order. A heuristic: a name that
// appears in a task's code and was defined by an earlier task counts as used. A local
// variable inside a function with the same name as a global one can make a false link; a
// variable reached through get() or an environment is missed. A sourced script is read as it
// is now, which may differ from when it ran.
func LinkVars(tasks []*Task) {
	lastDef := map[string]string{}
	for _, t := range tasks {
		t.Uses = nil
		code := t.Code()
		// a sourced script's own code counts: step2.R using `pk` from the console is a use
		for _, src := range t.Sources {
			if src.Class == ClassProject {
				if b, err := os.ReadFile(src.Path); err == nil {
					code += "\n" + string(b)
				}
			}
		}
		if code != "" {
			for name := range identifiers(code) {
				if def, ok := lastDef[name]; ok && def != t.ID {
					t.Uses = append(t.Uses, VarUse{Name: name, Task: def})
				}
			}
			sortUses(t.Uses)
		}
		for _, v := range t.Vars {
			lastDef[v.Name] = t.ID
		}
	}
}

func sortUses(u []VarUse) {
	for i := 1; i < len(u); i++ {
		for j := i; j > 0 && u[j].Name < u[j-1].Name; j-- {
			u[j], u[j-1] = u[j-1], u[j]
		}
	}
}
