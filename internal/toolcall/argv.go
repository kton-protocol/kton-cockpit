// Package toolcall is what the recorder's kton-call and the cockpit's tool shim (ADR-014) both
// need to see one tool call alike: its command line as the key sees it, and the content store that
// keeps a call's files by their hash. It moved here unchanged from recorder/watch, so call keys and
// ~/.cache/kton-cockpit/cas made before stay valid.
package toolcall

import (
	"os"
	"path/filepath"
	"strings"
)

// ScriptRunners are programs a command line names before the program it runs (sh run.sh): the key
// sees the program they run.
var ScriptRunners = map[string]bool{"sh": true, "bash": true, "dash": true, "perl": true, "env": true}

// RepoRoot is the nearest ancestor of dir holding cockpit.config.json or a .git directory. A
// .git file (a submodule, a worktree) counts only when no such ancestor is above it: a path in
// the parent repository stays a path of that repository. A worktree inside the repository
// (.claude/worktrees/w) without a cockpit.config.json of its own so resolves to the main
// repository; with the checked-in one it is its own root. dir itself when there is none.
func RepoRoot(dir string) string {
	gitFile := ""
	for d := dir; d != "" && d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "cockpit.config.json")); err == nil {
			return d
		}
		if fi, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			if fi.IsDir() {
				return d
			}
			if gitFile == "" {
				gitFile = d
			}
		}
	}
	if gitFile != "" {
		return gitFile
	}
	return dir
}

// NormArgv is NormCmdline for a command line given as its arguments (a stub's own os.Args).
func NormArgv(args []string, cwd string) []string { return NormCmdline(ShellJoin(args), cwd) }

// SplitCmdline splits a command line as the shim writes it: arguments separated by spaces, an
// argument with other characters in single quotes ('run 1.mod', 'it'\”s').
func SplitCmdline(s string) []string {
	var out []string
	var b strings.Builder
	in, quoted := false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case quoted && c == '\'':
			quoted = false
		case quoted:
			b.WriteByte(c)
		case c == '\'':
			quoted, in = true, true
		case c == ' ' || c == '\t' || c == '\n':
			if in {
				out = append(out, b.String())
				b.Reset()
				in = false
			}
		case c == '\\' && i+1 < len(s) && s[i+1] == '\'':
			b.WriteByte('\'') // the \' between two quoted parts of 'it'\''s'
			i++
			in = true
		default:
			b.WriteByte(c)
			in = true
		}
	}
	if in {
		out = append(out, b.String())
	}
	return out
}

// NormCmdline: the command line as the key sees it. The program by name; an absolute path inside
// the repository (RepoRoot of the working directory) relative to the working directory, with
// ".." where it lies above; any other absolute path (an installation, a licence) by its file
// name, since it belongs to the environment. Also inside key=value. The recorder and kton-call
// both come here, so a call and its stub see the same command line.
func NormCmdline(cmdline, cwd string) []string {
	f := SplitCmdline(cmdline)
	for len(f) > 1 && ScriptRunners[filepath.Base(f[0])] && !strings.HasPrefix(f[1], "-") {
		f = f[1:]
	}
	out := make([]string, len(f))
	root := cwd
	if cwd != "" {
		root = RepoRoot(cwd)
	}
	norm := func(p string) string {
		if !strings.HasPrefix(p, "/") {
			return p
		}
		if cwd == "" {
			return filepath.Base(p)
		}
		if r, err := filepath.Rel(root, p); err != nil || r == ".." || strings.HasPrefix(r, "../") {
			return filepath.Base(p) // outside the repository: the environment
		}
		if r, err := filepath.Rel(cwd, p); err == nil {
			return r
		}
		return filepath.Base(p)
	}
	for i, a := range f {
		switch {
		case i == 0:
			a = filepath.Base(a)
		case strings.Contains(a, "=/"):
			k := a[:strings.Index(a, "=")+1]
			a = k + norm(a[len(k):])
		default:
			a = norm(a)
		}
		out[i] = a
	}
	return out
}

// ShellJoin quotes what a POSIX shell would split or expand.
func ShellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a != "" && strings.Trim(a, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:,+@%") == "" {
			out[i] = a
		} else {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(out, " ")
}
