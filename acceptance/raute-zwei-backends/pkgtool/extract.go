package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/gitmick/ktonpkg"
)

// Der Adapter vom Cockpit-Foton zur backendneutralen Ausführung (ADR-006). Er liest nur, was im
// Foton steht; was er daraus schließt, sind Tatsachen der Aufzeichnung, keine Vermutungen:
//
//   - Arbeitsort: ein Befehl der Form `cd <ordner> && …` lief in <ordner>; sonst im Repo.
//   - Slot: der Pfad relativ zum Arbeitsort.
//   - Kommandodatei: das erste Wort der Befehlszeile, das genau ein Eingabeslot ist — das Programm
//     steht vor seinen Argumenten.
//   - Step-Name: der letzte Teil des Arbeitsorts. Nur ein Name, keine Bedeutung.
//   - Mit --entrypoint <prog>: die Befehlszeile in der Paketform. Ein Paket trägt, wie improve,
//     das Image als erste Zeile und nicht den Interpreter — den stellt das Werkzeug (jam-r:
//     `--entrypoint Rscript` in seiner tool.json). Also: das erste Wort muss <prog> sein, es fällt
//     weg, und das Image aus dem envRef des Fotons kommt davor. Beginnt eine Befehlszeile nicht mit
//     <prog>, lehnt der Adapter ab, statt etwas anderes daraus zu machen.
//
// Grenze des Prototyps: die Befehlszeile wird an Leerzeichen geteilt, Anführungszeichen kennt er
// nicht.

type record struct {
	Record struct {
		ID     string `json:"id"`
		Cmd    string `json:"cmd"`
		EnvRef string `json:"envRef"`
		Inputs []struct {
			Path string   `json:"path"`
			Hash string   `json:"hash"`
			URI  []string `json:"uri"`
		} `json:"inputs"`
		Outputs []struct {
			Path string `json:"path"`
			Hash string `json:"hash"`
		} `json:"outputs"`
	} `json:"record"`
}

// imageOf macht aus einem envRef die Image-Angabe, wie improve sie in der ersten Zeile trägt:
// oci://docker.io/scinteco/jam-r@sha256:… → scinteco/jam-r@sha256:…
func imageOf(envRef string) string {
	s := strings.TrimPrefix(envRef, "oci://")
	return strings.TrimPrefix(s, "docker.io/")
}

func loadExecutions(file, entrypoint string) ([]ktonpkg.Execution, map[string]string) {
	raw, err := os.ReadFile(file)
	if err != nil {
		fail(1, "%v", err)
	}
	var recs []record
	if err := json.Unmarshal(raw, &recs); err != nil {
		fail(1, "%s: %v", file, err)
	}
	where := map[string]string{} // hash → Pfad im Repo, zum Holen der Bytes
	var out []ktonpkg.Execution
	for _, rr := range recs {
		r := rr.Record
		dir, rest := "", r.Cmd
		if strings.HasPrefix(rest, "cd ") {
			if d, after, ok := strings.Cut(strings.TrimPrefix(rest, "cd "), " && "); ok {
				dir, rest = d, after
			}
		}
		slot := func(p string) string {
			if dir != "" && strings.HasPrefix(p, dir+"/") {
				return strings.TrimPrefix(p, dir+"/")
			}
			return p
		}
		e := ktonpkg.Execution{ID: r.ID, Name: path.Base(dir), Command: strings.Fields(rest),
			Environment: r.EnvRef}
		if dir == "" {
			e.Name = ""
		}
		inSlots := map[string]bool{}
		for _, f := range r.Inputs {
			e.Inputs = append(e.Inputs, ktonpkg.ExecFile{Slot: slot(f.Path), Hash: f.Hash})
			inSlots[slot(f.Path)] = true
			where[f.Hash] = f.Path
		}
		for _, f := range r.Outputs {
			e.Outputs = append(e.Outputs, ktonpkg.ExecFile{Slot: slot(f.Path), Hash: f.Hash})
			where[f.Hash] = f.Path
		}
		for _, tok := range e.Command {
			if inSlots[tok] {
				e.CommandFile = tok
				break
			}
		}
		if entrypoint != "" {
			if len(e.Command) == 0 || e.Command[0] != entrypoint {
				fail(2, "%s: die Befehlszeile %q beginnt nicht mit %s", r.ID, rest, entrypoint)
			}
			if r.EnvRef == "" {
				fail(2, "%s: ohne envRef gibt es kein Image für die erste Zeile", r.ID)
			}
			e.Command = append([]string{imageOf(r.EnvRef)}, e.Command[1:]...)
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, where
}

// entrypointFlag liest `--entrypoint <prog>` aus args und gibt den Rest zurück.
func entrypointFlag(args []string) (string, []string) {
	var rest []string
	ep := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--entrypoint" && i+1 < len(args) {
			ep = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return ep, rest
}

func propose(file string, args []string) {
	ep, _ := entrypointFlag(args)
	execs, _ := loadExecutions(file, ep)
	p, err := ktonpkg.Propose(execs)
	if err != nil {
		fail(1, "%v", err)
	}
	emit(p)
}

func extract(recFile, choiceFile, dest string, args []string) {
	ep, args := entrypointFlag(args)
	execs, where := loadExecutions(recFile, ep)
	p, err := ktonpkg.Propose(execs)
	if err != nil {
		fail(1, "%v", err)
	}
	var c ktonpkg.Choice
	raw, err := os.ReadFile(choiceFile)
	if err != nil {
		fail(1, "%v", err)
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		fail(1, "%s: %v", choiceFile, err)
	}
	// Die Bytes kommen aus dem Arbeitsbaum. Eine föderierte Ausführung brächte sie über ihren
	// Locator; Extract prüft sie in beiden Fällen gegen den Hash.
	fetch := func(h string) ([]byte, error) {
		p, ok := where[h]
		if !ok {
			return nil, fmt.Errorf("keine Datei zu diesem Hash im Arbeitsbaum")
		}
		return os.ReadFile(p)
	}
	if err := os.RemoveAll(dest); err != nil {
		fail(1, "%v", err)
	}
	b, err := ktonpkg.Extract(p, execs, c, fetch, dest)
	if err != nil {
		fail(2, "%v", err)
	}
	out := map[string]any{"package": dest, "derivedFrom": ktonpkg.DerivedFrom(p, c.Reference)}
	if len(args) == 2 && args[0] == "--sign" {
		s, err := ktonpkg.OpenOrCreateSigner(args[1])
		if err != nil {
			fail(1, "%v", err)
		}
		rayID, err := b.Ray.ID()
		if err != nil {
			fail(1, "%v", err)
		}
		var ev []ktonpkg.Ref
		for _, id := range ktonpkg.DerivedFrom(p, c.Reference) {
			ev = append(ev, ktonpkg.Ref{Hash: id, Name: "Ausführung"})
		}
		if err := b.AddClaim(s, ktonpkg.Claim{
			Subject:   ktonpkg.Ref{Hash: rayID, Name: c.Name},
			Predicate: "http://www.w3.org/ns/prov#wasDerivedFrom",
			Object:    map[string]any{"reference": c.Reference},
			Evidence:  ev,
			Why:       "aus diesen Ausführungen extrahiert (kton-cockpit ADR-006)",
		}); err != nil {
			fail(1, "%v", err)
		}
		if err := b.Write(); err != nil {
			fail(1, "%v", err)
		}
		out["rayId"] = rayID
	}
	if id, err := b.ID(); err == nil {
		out["bundleId"] = id
	}
	var problems []string
	for _, pr := range b.Verify() {
		problems = append(problems, pr.String())
	}
	out["problems"] = problems
	emit(out)
}
