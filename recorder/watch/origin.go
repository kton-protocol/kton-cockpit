package watch

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ParseOrigins reads the JSON lines the Positron extension writes, one execution per line.
// It accepts the recorder's own format and the raw format of the S6 probe
// ({"ts": ms, "event": "executed", "payload": <onDidExecuteCode event>}). R and Python
// executions are kept, each with its language; others are skipped.
func ParseOrigins(path string) ([]Origin, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Origin
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		if o, ok := parseOriginLine(sc.Bytes()); ok {
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out, sc.Err()
}

type rawPos struct {
	Line int `json:"line"`
}
type rawLoc struct {
	URI struct {
		Path string `json:"path"`
	} `json:"uri"`
	Range struct {
		Start rawPos `json:"start"`
		End   rawPos `json:"end"`
	} `json:"range"`
}
type rawLine struct {
	// recorder format
	TSns      int64  `json:"ts_ns"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Code      string `json:"code"`
	Language  string `json:"language"`
	// probe format
	TS      int64  `json:"ts"`
	Event   string `json:"event"`
	Payload *struct {
		Code        string `json:"code"`
		LanguageID  string `json:"languageId"`
		Attribution struct {
			Source   string `json:"source"`
			Metadata *struct {
				CodeLocation *rawLoc `json:"codeLocation"`
			} `json:"metadata"`
		} `json:"attribution"`
	} `json:"payload"`
}

func parseOriginLine(b []byte) (Origin, bool) {
	var r rawLine
	if json.Unmarshal(b, &r) != nil {
		return Origin{}, false
	}
	if r.Payload != nil {
		lang, ok := originLanguage(r.Payload.LanguageID)
		if r.Event != "executed" || !ok {
			return Origin{}, false
		}
		o := Origin{Kind: r.Payload.Attribution.Source, Code: r.Payload.Code, TS: r.TS * 1_000_000, Language: lang}
		if m := r.Payload.Attribution.Metadata; m != nil && m.CodeLocation != nil {
			o.File = m.CodeLocation.URI.Path
			s, e := m.CodeLocation.Range.Start.Line, m.CodeLocation.Range.End.Line
			if s != 0 || e != 0 { // 0:0–0:0 is what Positron sends for a Quarto cell: unknown
				o.StartLine, o.EndLine = s+1, e+1
			}
		}
		return o, true
	}
	lang, ok := originLanguage(r.Language)
	if r.TSns == 0 || !ok {
		return Origin{}, false
	}
	return Origin{Kind: r.Kind, File: r.File, StartLine: r.StartLine, EndLine: r.EndLine, Code: r.Code, TS: r.TSns, Language: lang}, true
}

// originLanguage: "r" (also for a line that names none, as the first extension wrote them) or
// "python"; ok is false for any other language.
func originLanguage(l string) (string, bool) {
	switch l {
	case "", "r":
		return "r", true
	case "python":
		return "python", true
	}
	return "", false
}

// OriginsIn keeps the executions of one language: a session's tasks get only its own console's.
func OriginsIn(origins []Origin, lang string) []Origin {
	if lang == "" {
		lang = "r"
	}
	var out []Origin
	for _, o := range origins {
		if o.Language == lang {
			out = append(out, o)
		}
	}
	return out
}

// AttachOrigins gives each task the executions that were submitted while it was the current
// one: after the previous marker, up to its own. Ark sets a marker after every execution, so
// that is normally one origin per task; a task that is only Ark's own bookkeeping gets none.
//
// A task with an origin also knows when its command was sent. Before that the console was
// waiting for input, and what the process read then was not the command's: Ark's language
// server indexes the workspace's R files at start and reads a file again after it ran. Such
// reads are dropped (a file the command read too keeps its later read). Only the kind of file
// Ark reads, though: the extension notes the time only after the command was handed over, so a
// command's own first reads can lie before it, and a data file is never dropped for that.
func AttachOrigins(tasks []*Task, origins []Origin) {
	for _, t := range tasks {
		t.Origins = nil
	}
	// A sourced script is matched by its file first: R names it in the step that ran it, in the
	// log's order, while the clocks may disagree by tenths of a second (seen in WSL: the source
	// line stamped 0.1 s after the step's own end), which put the command into the next step.
	placed := make([]bool, len(origins))
	for k, o := range origins {
		if o.File == "" {
			continue
		}
		var best *Task
		var bestD int64
		for _, t := range tasks {
			if !sourced(t, o.File) || o.TS < t.Start-sourceSlack || o.TS > t.End+sourceSlack || hasOriginOf(t, o.File) {
				continue
			}
			d := int64(0)
			if o.TS < t.Start {
				d = t.Start - o.TS
			} else if o.TS > t.End {
				d = o.TS - t.End
			}
			if best == nil || d < bestD {
				best, bestD = t, d
			}
		}
		if best != nil {
			resolveChunk(&o)
			best.Origins = append(best.Origins, o)
			placed[k] = true
		}
	}
	// everything else by time: the step that was running when it was sent
	i := 0
	for _, t := range tasks {
		for i < len(origins) && origins[i].TS <= t.End {
			if !placed[i] && origins[i].TS >= t.Start {
				o := origins[i]
				resolveChunk(&o)
				t.Origins = append(t.Origins, o)
			}
			i++
		}
		sort.SliceStable(t.Origins, func(a, b int) bool { return t.Origins[a].TS < t.Origins[b].TS })
	}
	for _, t := range tasks {
		if len(t.Origins) > 0 {
			sent := t.Origins[0].TS - idleSlack
			kept := t.Inputs[:0]
			for _, in := range t.Inputs {
				if in.LastRead == 0 || in.LastRead >= sent || !indexedByArk(in.Path) {
					kept = append(kept, in)
				}
			}
			t.Inputs = kept
		}
	}
}

// sourceSlack: how far a script's origin may lie outside the step that sourced it.
const sourceSlack = 2_000_000_000

func sourced(t *Task, file string) bool {
	for _, s := range t.Sources {
		if s.Path == file {
			return true
		}
	}
	return t.Script == file
}

func hasOriginOf(t *Task, file string) bool {
	for _, o := range t.Origins {
		if o.File == file {
			return true
		}
	}
	return false
}

// indexedByArk: the files Ark's language server reads while the console waits: R and Quarto
// sources, and the ignore files it consults to know which ones.
func indexedByArk(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".r", ".qmd", ".rmd":
		return true
	}
	return filepath.Base(p) == ".gitignore" || strings.HasSuffix(p, "/git/ignore")
}

// idleSlack: the extension's clock and the shim's are the same machine's, but the extension
// writes milliseconds and notes the execution a moment after sending it.
const idleSlack = 200_000_000

var chunkStart = regexp.MustCompile("^\\s*```+\\s*\\{r([ ,][^}]*)?\\}\\s*$")
var chunkLabel = regexp.MustCompile(`^\s*#\|\s*label:\s*(\S+)`)

// resolveChunk finds which chunk of a Quarto/R Markdown document the code was: Positron sends
// the file but no lines for a cell (S6), so the code is matched against the document's chunks.
func resolveChunk(o *Origin) {
	if o.File == "" || o.StartLine != 0 || !(strings.HasSuffix(o.File, ".qmd") || strings.HasSuffix(o.File, ".Rmd") || strings.HasSuffix(o.File, ".rmd")) {
		return
	}
	b, err := os.ReadFile(o.File)
	if err != nil {
		return
	}
	want := normCode(o.Code)
	for _, c := range Chunks(string(b)) {
		if normCode(c.Code) == want || (want != "" && strings.Contains(normCode(c.Code), want)) {
			o.Chunk = c.Label
			o.StartLine, o.EndLine = c.StartLine, c.EndLine
			return
		}
	}
}

func normCode(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#|") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

// Chunk is one R code chunk of a Quarto or R Markdown document.
type Chunk struct {
	Label              string
	StartLine, EndLine int // body lines, 1-based
	Code               string
}

// Chunks lists the R chunks of a document. A chunk without a label is called "#n".
func Chunks(doc string) []Chunk {
	var out []Chunk
	lines := strings.Split(doc, "\n")
	n := 0
	for i := 0; i < len(lines); i++ {
		m := chunkStart.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		n++
		label := ""
		if opts := strings.TrimSpace(strings.TrimLeft(m[1], " ,")); opts != "" {
			if first := strings.TrimSpace(strings.SplitN(opts, ",", 2)[0]); first != "" && !strings.Contains(first, "=") {
				label = first
			}
		}
		start := i + 2
		var body []string
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
			if lm := chunkLabel.FindStringSubmatch(lines[i]); lm != nil {
				label = lm[1]
			}
			body = append(body, lines[i])
		}
		if label == "" {
			label = "#" + itoa(n)
		}
		out = append(out, Chunk{Label: label, StartLine: start, EndLine: start + len(body) - 1, Code: strings.Join(body, "\n")})
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
