// Package watch turns the event log written by libktontrace.so into tasks: which files a
// command read and wrote (with their content hash), which programs it started, which network
// it touched. It executes nothing and decides nothing about meaning; that is left to whoever
// looks at the result (Watch → Select → Document).
package watch

import (
	"strconv"
	"strings"
)

// Event is one line of the shim's log:
//
//	ts_ns pid ppid op mode path size mtime_ns ino extra
type Event struct {
	TS    int64
	PID   int
	PPID  int
	Op    string
	Mode  string
	Path  string
	Size  int64
	Mtime int64
	Ino   uint64
	Extra string
}

// Header is the first line of a log, written by the process that created it.
type Header struct {
	Version string
	Hash    []string // $KTON_TRACE_HASH prefixes; the first one is taken as the project
}

// ParseHeader reads "#ktontrace v2<TAB>hash=a:b" (or v1). ok is false for any other line.
func ParseHeader(line string) (Header, bool) {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "#ktontrace ") {
		return Header{}, false
	}
	f := strings.Split(strings.TrimPrefix(line, "#ktontrace "), "\t")
	h := Header{Version: f[0]}
	for _, kv := range f[1:] {
		if v, ok := strings.CutPrefix(kv, "hash="); ok && v != "" {
			h.Hash = strings.Split(v, ":")
		}
	}
	return h, true
}

// Escaped reports whether a log of this version escapes path and extra (v2: a backslash, TAB,
// LF and CR as \\, \t, \n, \r). v1 wrote them as a space; that cannot be undone.
func (h Header) Escaped() bool { return h.Version != "" && h.Version != "v1" }

// Quoted reports whether exec and spawn lines quote their arguments (v3). Before, they were
// joined by spaces, and a quote character in an argument was just a character.
func (h Header) Quoted() bool { return h.Escaped() && h.Version != "v2" }

// Escape writes a path or extra field the v2 way, for programs that add lines to a log.
func Escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`).Replace(s)
}

// Unescape undoes the v2 escaping of a path or extra field.
func Unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// ParseEscaped reads one event line of a v2 log: path and extra are unescaped.
func ParseEscaped(line string) (Event, bool) {
	e, ok := Parse(line)
	if ok {
		e.Path, e.Extra = Unescape(e.Path), Unescape(e.Extra)
	}
	return e, ok
}

// Parse reads one event line as written (v1). Comment lines and short lines are not events.
func Parse(line string) (Event, bool) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" || line[0] == '#' {
		return Event{}, false
	}
	f := strings.SplitN(line, "\t", 10)
	if len(f) < 10 {
		return Event{}, false
	}
	ts, _ := strconv.ParseInt(f[0], 10, 64)
	pid, _ := strconv.Atoi(f[1])
	ppid, _ := strconv.Atoi(f[2])
	size, _ := strconv.ParseInt(f[6], 10, 64)
	mt, _ := strconv.ParseInt(f[7], 10, 64)
	ino, _ := strconv.ParseUint(f[8], 10, 64)
	extra := f[9]
	if extra == "-" {
		extra = ""
	}
	return Event{ts, pid, ppid, f[3], f[4], f[5], size, mt, ino, extra}, true
}

// KV returns a key from a ';'-separated "k=v" extra field ("sha256=…;new=1").
func (e Event) KV(k string) string {
	for _, f := range strings.Split(e.Extra, ";") {
		if v, ok := strings.CutPrefix(f, k+"="); ok {
			return v
		}
	}
	return ""
}

// EmptySHA256 is the hash of zero bytes. A file with this hash connects nothing to anything.
const EmptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
