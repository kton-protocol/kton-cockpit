package watch

import (
	"fmt"
	"io"
)

func short(h string) string {
	switch {
	case h == "":
		return "------------"
	case len(h) > 12:
		return h[:12]
	}
	return h
}

// Report prints tasks as text, one block per task.
func Report(w io.Writer, s *Session, tasks []*Task) {
	rel := s.cls.Rel
	for _, t := range tasks {
		status := ""
		if t.Status == "err" {
			status = "  ⚠ Fehler"
		}
		fmt.Fprintf(w, "\n━━ Befehl %s  %s%s\n", t.ID, t.Expr, status)
		for _, o := range t.Origins {
			where := o.Kind
			if o.File != "" {
				where = rel(o.File)
				if o.Chunk != "" {
					where += " · Chunk " + o.Chunk
				} else if o.StartLine > 0 {
					where += fmt.Sprintf(" · Zeile %d–%d", o.StartLine, o.EndLine)
				}
			}
			fmt.Fprintf(w, "   HERKUNFT %s\n", where)
		}
		for _, v := range t.Vars {
			fmt.Fprintf(w, "   VARIABLE %s  %s\n", v.Name, v.Desc)
		}
		for _, src := range t.Sources {
			fmt.Fprintf(w, "   SOURCE  %s\n", rel(src.Path))
		}
		if len(t.PIDs) > 1 {
			fmt.Fprintf(w, "   Prozesse: %v\n", t.PIDs)
		}
		for _, c := range t.Commands {
			fmt.Fprintf(w, "   gestartet  [pid %d] %s\n", c.PID, c.Cmdline)
		}
		for _, c := range t.Unrecorded {
			fmt.Fprintf(w, "   NICHT AUFGEZEICHNET [pid %d] %s  ⚠ statisch gelinkt oder gescheitert\n", c.PID, c.Program)
		}
		for _, in := range t.Inputs {
			tag := ""
			if in.Class == ClassOutside {
				tag += "  (außerhalb des Projekts)"
			}
			if in.Status != "observed" {
				tag += "  ⚠ " + in.Status
			}
			from := ""
			for _, r := range in.From {
				from += fmt.Sprintf("  ← Befehl %s", r.Task)
			}
			for _, r := range in.SameAs {
				from += fmt.Sprintf("  (gleicher Inhalt wie %s aus Befehl %s)", rel(r.Path), r.Task)
			}
			fmt.Fprintf(w, "   INPUT   %s  %s  (pid %d)%s%s\n", short(in.SHA256), rel(in.Path), in.PID, from, tag)
		}
		for _, m := range t.Missing {
			fmt.Fprintf(w, "   FEHLT   %s\n", rel(m))
		}
		for _, l := range t.Listings {
			fmt.Fprintf(w, "   LISTING %s\n", rel(l))
		}
		for _, o := range t.Outputs {
			tag := ""
			if o.Overwritten {
				tag = "  ⚠ später überschrieben"
			}
			if o.Status != "observed" {
				tag += "  ⚠ " + o.Status
			}
			fmt.Fprintf(w, "   OUTPUT  %s  %s%s\n", short(o.SHA256), rel(o.Path), tag)
		}
		for _, p := range t.Temporary {
			fmt.Fprintf(w, "   temporär %s\n", rel(p))
		}
		for _, n := range t.Network {
			note := ""
			if n.Class == "remote" {
				note = "  ⚠ kein Foton – braucht einen Claim"
			}
			fmt.Fprintf(w, "   NETZ    [pid %d] %s (%s)%s\n", n.PID, n.Address, n.Class, note)
		}
		fmt.Fprintf(w, "   (%d Dateien der Umgebung gelesen)\n", t.EnvFiles)
	}
}
