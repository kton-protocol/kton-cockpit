package watch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Draft is a proposal for documenting one observed task with `cockpit publish`: the files
// it read and wrote, relative to the repository, and a command line describing it. Nothing
// is decided here; the person selects and edits, then runs the verb by hand.
type Draft struct {
	Repo    string      `json:"repo"`   // repository root the paths are relative to
	Inputs  []DraftFile `json:"inputs"` // candidates; Use says whether they are preselected
	Outputs []DraftFile `json:"outputs"`
	Cmd     string      `json:"cmd"`
	Notes   []string    `json:"notes,omitempty"` // what cannot be a foton input and needs a claim
}

type DraftFile struct {
	Path string `json:"path"` // repo-relative
	Use  bool   `json:"use"`
	Note string `json:"note,omitempty"`
}

// RepoRoot is the nearest ancestor of dir holding .git or cockpit.config.json; dir itself
// when there is none.
func RepoRoot(dir string) string {
	for d := dir; d != "" && d != "/" && d != "."; d = filepath.Dir(d) {
		for _, m := range []string{".git", "cockpit.config.json"} {
			if _, err := os.Stat(filepath.Join(d, m)); err == nil {
				return d
			}
		}
	}
	return dir
}

var toolCmd = regexp.MustCompile(`nmfe|execute|bbi|quarto|Rscript`)

// MakeDraft proposes what to publish for a task.
func MakeDraft(s *Session, t *Task) Draft {
	repo := RepoRoot(s.Project)
	d := Draft{Repo: repo, Cmd: draftCmd(t, repo)}
	file := func(p string, cls Class, extra string) DraftFile {
		r, err := filepath.Rel(repo, p)
		if err != nil || strings.HasPrefix(r, "..") {
			return DraftFile{Path: p, Note: "nicht im Repo: cockpit publish nimmt heute nur Pfade im Repo"}
		}
		f := DraftFile{Path: r, Use: cls == ClassProject, Note: extra}
		return f
	}
	for _, in := range t.Inputs {
		if in.SHA256 == EmptySHA256 {
			continue
		}
		note := ""
		if in.Status == "changed-outside" {
			note = "ohne Aufzeichnung geändert"
		}
		f := file(in.Path, in.Class, note)
		if in.Rewritten || Scratch(in.Path) {
			f.Use = false
			f.Note = "Zwischendatei des Werkzeugs"
		}
		nowNote(&f, in.Now)
		d.Inputs = append(d.Inputs, f)
	}
	for _, o := range t.Outputs {
		if o.SHA256 == EmptySHA256 {
			continue
		}
		f := file(o.Path, o.Class, "")
		if Scratch(o.Path) {
			f.Use, f.Note = false, "Zwischendatei des Werkzeugs"
		}
		nowNote(&f, o.Now)
		d.Outputs = append(d.Outputs, f)
	}
	for _, n := range t.Network {
		if n.Class == "remote" {
			d.Notes = append(d.Notes, "Netzzugriff "+n.Address+": kein Foton, braucht einen Claim")
		}
	}
	for _, u := range t.Unrecorded {
		d.Notes = append(d.Notes, "nicht aufgezeichnetes Programm "+u.Program+": seine Dateien fehlen in der Auswahl")
	}
	return d
}

func draftCmd(t *Task, repo string) string {
	rel := func(p string) string {
		if r, err := filepath.Rel(repo, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
		return p
	}
	if len(t.Origins) > 0 && t.Origins[0].File != "" {
		o := t.Origins[0]
		switch {
		case o.Chunk != "":
			return "chunk " + o.Chunk + " of " + rel(o.File)
		case o.StartLine > 0 && !(strings.HasPrefix(strings.TrimSpace(o.Code), "source(")):
			return rel(o.File) + ":" + itoa(o.StartLine) + "-" + itoa(o.EndLine)
		}
		return `source("` + rel(o.File) + `")`
	}
	for _, s := range t.Sources {
		if s.Class == ClassProject {
			return `source("` + rel(s.Path) + `")`
		}
	}
	if t.Expr != "" && t.ID != "tail" && !strings.HasPrefix(t.Expr, "base::") {
		return t.Expr // what the person typed, e.g. system("nmfe75 …")
	}
	for _, c := range t.Commands {
		if toolCmd.MatchString(c.Cmdline) {
			return c.Cmdline
		}
	}
	if t.ID == "tail" && len(t.Commands) > 0 {
		return t.Commands[0].Cmdline // no markers (a shell script): the command the session ran
	}
	return ""
}

// nowNote: publish hashes the file as it is today. A file that changed or vanished since it
// was observed would register other bytes than the ones that took part, so it is not
// preselected.
func nowNote(f *DraftFile, now string) {
	switch now {
	case NowChanged:
		f.Use, f.Note = false, "seit der Beobachtung geändert: publish würde den neuen Inhalt registrieren"
	case NowGone:
		f.Use, f.Note = false, "nicht mehr vorhanden"
	}
}

// CallDraft proposes a `cockpit publish` for one tool call (an nmfe run, a PsN vpc): exactly the
// files the call read and left, and its command line run from its working directory. ADR-010
// needs such a record for every nmfe call an orchestrator makes; the recorder sees each one.
// now says whether a file still has the observed bytes (Checker.State); nil skips the check.
func CallDraft(c Call, repo string, now func(path, sum string) string) Draft {
	cwd := "."
	if r, err := filepath.Rel(repo, c.Cwd); err == nil && !strings.HasPrefix(r, "..") {
		cwd = r
	}
	d := Draft{Repo: repo, Cmd: shellJoin(c.Argv)}
	if cwd != "." {
		d.Cmd = "cd " + shellJoin([]string{cwd}) + " && " + d.Cmd
	}
	file := func(f CallFile) DraftFile {
		r, err := filepath.Rel(repo, f.Path)
		if err != nil || strings.HasPrefix(r, "..") {
			return DraftFile{Path: f.Path, Note: "nicht im Repo: cockpit publish nimmt heute nur Pfade im Repo"}
		}
		df := DraftFile{Path: r, Use: true}
		if Scratch(f.Path) {
			df.Use, df.Note = false, "Zwischendatei des Werkzeugs"
		}
		if now != nil && df.Use {
			nowNote(&df, now(f.Path, f.SHA256))
		}
		return df
	}
	for _, f := range c.Reads {
		if f.SHA256 != EmptySHA256 {
			d.Inputs = append(d.Inputs, file(f))
		}
	}
	for _, f := range c.Writes {
		if f.SHA256 != EmptySHA256 {
			d.Outputs = append(d.Outputs, file(f))
		}
	}
	for _, f := range c.Outside {
		d.Notes = append(d.Notes, "liest außerhalb des Projekts "+f.Path+": kein Foton-Input, gehört zur Umgebung oder braucht einen Claim")
	}
	for _, f := range c.Missing {
		d.Notes = append(d.Notes, "suchte "+f.Rel+" und fand es nicht")
	}
	if c.Parent != 0 {
		d.Notes = append(d.Notes, "läuft innerhalb eines anderen Aufrufs (pid "+itoa(c.Parent)+")")
	}
	if len(d.Outputs) == 0 {
		d.Notes = append(d.Notes, "hinterlässt keine Datei: nichts zu veröffentlichen")
	}
	return d
}

// shellJoin quotes what a POSIX shell would split or expand.
func shellJoin(args []string) string {
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
