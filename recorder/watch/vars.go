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
	rIdent = regexp.MustCompile(`[A-Za-z.][A-Za-z0-9._]*`)
)

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

// Code is the code a task ran as far as it is known: every command Positron sent in it, in the
// order sent (a command that closed no task of its own, getwd() typed before a source(), ran
// inside this one), else the expression R saw (not Ark's own `.ark_last_value`).
func (t *Task) Code() string {
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

// usedNames: the names R code reads as variables (strings, comments, `$col`, `@slot` and
// `pkg::fun` aside). Not a call (`c(...)` is the function, whatever
// a session left in a variable called c), not a name the code assigns before it reads it
// (`pop <- read.csv(...)` in a script is the script's own pop), not a named argument (`f(x = 1)`),
// not a function's parameter. A name changed in place (`x$y <- 1`, `x[1] <- 2`) is read: it must
// exist already.
var rFunctionParams = regexp.MustCompile(`function\s*\(([^)]*)\)`)

// stripRCode: R code without its comments, and with every string literal emptied (""), read in one
// pass as R reads it: a quote inside a comment opens nothing ("the fit's own data"), a # inside a
// string starts no comment. Backquoted names stay: `my var` is a name.
func stripRCode(code string) string {
	var b strings.Builder
	for i := 0; i < len(code); i++ {
		switch c := code[i]; c {
		case '#':
			for i < len(code) && code[i] != '\n' {
				i++
			}
			if i < len(code) {
				b.WriteByte('\n')
			}
		case '"', '\'':
			for i++; i < len(code) && code[i] != c; i++ {
				if code[i] == '\\' {
					i++
				}
			}
			b.WriteString(`""`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// R's reserved words and constants: never a variable
var rReserved = map[string]bool{"if": true, "else": true, "for": true, "while": true, "repeat": true, "in": true,
	"next": true, "break": true, "function": true, "TRUE": true, "FALSE": true, "NULL": true, "NA": true, "NaN": true,
	"Inf": true, "NA_integer_": true, "NA_real_": true, "NA_character_": true, "T": true, "F": true}

func usedNames(code string) map[string]bool {
	code = stripRCode(code)
	params := map[string]bool{}
	for _, m := range rFunctionParams.FindAllStringSubmatch(code, -1) {
		for _, p := range strings.Split(m[1], ",") {
			if name, _, _ := strings.Cut(p, "="); strings.TrimSpace(name) != "" {
				params[strings.TrimSpace(name)] = true
			}
		}
	}
	depth := make([]int, len(code)+1) // parentheses open at each position
	for i, d := 0, 0; i < len(code); i++ {
		depth[i] = d
		switch code[i] {
		case '(':
			d++
		case ')':
			if d > 0 {
				d--
			}
		}
	}
	used, local := map[string]bool{}, map[string]bool{}
	for _, m := range rIdent.FindAllStringIndex(code, -1) {
		if m[0] > 0 {
			if p := code[m[0]-1]; p == '$' || p == '@' || p == ':' {
				continue
			}
		}
		name := code[m[0]:m[1]]
		if params[name] || rReserved[name] {
			continue
		}
		rest := strings.TrimLeft(code[m[1]:], " \t")
		before := strings.TrimRight(code[:m[0]], " \t")
		switch {
		case strings.HasPrefix(rest, "("):
			continue // a call
		case strings.HasPrefix(rest, "<-") || strings.HasPrefix(rest, "<<-") || strings.HasSuffix(before, "->"),
			strings.HasPrefix(rest, "=") && !strings.HasPrefix(rest, "==") && depth[m[0]] == 0:
			if !used[name] {
				local[name] = true // assigned before any read: the code's own
			}
			continue
		case strings.HasPrefix(rest, "=") && !strings.HasPrefix(rest, "=="):
			continue // a named argument
		}
		if !local[name] {
			used[name] = true
		}
	}
	return used
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
			for name := range usedNames(code) {
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
