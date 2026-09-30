// pkgtool ist der Teil der git-Seite, den das Cockpit noch nicht kann: ein kton-Paket laden, prüfen,
// seine Bedingung lesen und einen Workflow mit gebundenen Löchern in Befehle für `cockpit publish`
// übersetzen.
//
// Es rechnet nichts selbst. Paket- und Ray-Identität, Reihenfolge, Parameterauflösung und die Frage,
// welche Löcher offen sind, kommen aus ktonpkg — derselben Bibliothek, die improvego auf der anderen
// Seite benutzt. Im Zielbild (kton-cockpit ADR-005, S5/S6) wird genau das zu `cockpit publish` mit
// `from` und `bindings`; bis dahin steht es hier, sichtbar als Lücke.
//
//	pkgtool inspect <paket>                              Art, Name, Identität, Prüfbefund, Löcher, Bedingungen, Spectrum
//	pkgtool plan <paket> <arbeitsordner> [--no-defaults] [--bind name=wert ...]
//	                                                     die Steps in Reihenfolge, als Befehle; offene Pflichtlöcher → Exit 2
//	pkgtool foton <paket> <step> [--bind loch=sha256:… ...] [--output datei=pfad ...]
//	                                                     der Step als Foton-Spec für `plankton author`: ohne Bindungen
//	                                                     das Potential, mit Bindungen und Ausgaben eine Realisierung
//	pkgtool potentials <paket> [--bind loch=wert ...] [--upstream ausgaben.json]
//	                                                     je Step Potential-Id, Protokoll-Ref und — gebunden — Aktionsschlüssel
//	pkgtool propose <records.json> [--entrypoint prog]   Vorschlag: Läufe, Steps, Kandidaten (ADR-006)
//	pkgtool extract <records.json> <choice.json> <ziel> [--entrypoint prog] [--sign schlüssel]
//	                                                     das gewählte Potential als Ray-Paket, mit prov:wasDerivedFrom
//
// records.json ist eine Liste von Antworten auf `cockpit ask {query: record}` — nur verifizierte
// Ausführungen, weil nur die dort herauskommen.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/kton-protocol/ktonpkg"
)

func main() {
	if len(os.Args) < 3 {
		fail(1, "usage: pkgtool inspect|plan|foton|propose|extract … (siehe Kopf von main.go)")
	}
	switch os.Args[1] {
	case "propose":
		propose(os.Args[2], os.Args[3:])
		return
	case "extract":
		if len(os.Args) < 5 {
			fail(1, "extract <records.json> <choice.json> <ziel> [--sign schlüssel]")
		}
		extract(os.Args[2], os.Args[3], os.Args[4], os.Args[5:])
		return
	}
	b, err := ktonpkg.Open(os.Args[2])
	if err != nil {
		fail(1, "%v", err)
	}
	switch os.Args[1] {
	case "inspect":
		inspect(b)
	case "plan":
		if len(os.Args) < 4 {
			fail(1, "plan braucht den Arbeitsordner (repo-relativ)")
		}
		plan(b, os.Args[3], os.Args[4:])
	case "potentials":
		potentials(b, os.Args[3:])
	case "foton":
		if len(os.Args) < 4 {
			fail(1, "foton braucht den Step")
		}
		fotonSpec(b, os.Args[3], os.Args[4:])
	default:
		fail(1, "unbekannt: %s", os.Args[1])
	}
}

// potentials nennt je Step die Identität des Potentials: die Foton-Id mit offenen Löchern und die
// Protokoll-Ref. Mit Bindungen (Datei-Löcher als Inhaltshash) und den Ausgaben der Vorgänger
// (ausgaben.json: "<step>/<datei>" → Hash, wie results/*.json sie führt) dazu den Aktionsschlüssel —
// der rechnet über Eingaben und Protokoll, nicht über Ausgaben, und entscheidet über Reuse.
func potentials(b *ktonpkg.Bundle, args []string) {
	bindings := ktonpkg.Bindings{}
	var upstream map[string]string
	for i := 0; i+1 < len(args); i += 2 {
		switch args[i] {
		case "--bind":
			k, v, _ := strings.Cut(args[i+1], "=")
			bindings[k] = v
		case "--upstream":
			raw, err := os.ReadFile(args[i+1])
			if err != nil {
				fail(1, "%v", err)
			}
			var outs map[string]string
			if err := json.Unmarshal(raw, &outs); err != nil {
				fail(1, "%v", err)
			}
			upstream = map[string]string{}
			for k, v := range outs {
				step, file, _ := strings.Cut(k, "/")
				upstream[step+":"+file] = v
			}
		default:
			fail(1, "unbekannt: %s", args[i])
		}
	}
	out := map[string]any{}
	for _, s := range b.Ray.Steps {
		f, err := b.Ray.Foton(s.ID, nil, nil)
		if err != nil {
			fail(1, "%v", err)
		}
		id, err := f.FotonID()
		if err != nil {
			fail(1, "%v", err)
		}
		e := map[string]string{"potential": id, "protocolRef": f.Protocol.Ref}
		if upstream != nil {
			ak, err := b.Ray.ActionKey(s.ID, bindings, upstream)
			if err != nil {
				e["actionKeyError"] = err.Error()
			} else {
				e["actionKey"] = ak
			}
		}
		out[s.ID] = e
	}
	emit(out)
}

// fotonSpec gibt den Step als foton-Spec aus, wie `plankton author <spec.json>` sie liest. Die
// Identität rechnet ktonpkg (Ray.Foton) — dieselbe Funktion, mit der improvego den Step auf der
// anderen Seite aufzeichnet. Darum trägt ein hier realisierter Normalisierer dieselbe Protokoll-Ref
// wie das Potential im Spectrum, und nur daran erkennt der Kernel eine Normalisierung an.
func fotonSpec(b *ktonpkg.Bundle, step string, args []string) {
	if b.Ray == nil {
		fail(1, "%s hat keinen Ray", b.Package.Name)
	}
	bindings := ktonpkg.Bindings{}
	outputs := map[string]string{}
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) || !strings.Contains(args[i+1], "=") {
			fail(1, "%s erwartet name=wert", args[i])
		}
		k, v, _ := strings.Cut(args[i+1], "=")
		switch args[i] {
		case "--bind":
			bindings[k] = v
		case "--output":
			outputs[k] = v
		default:
			fail(1, "unbekannt: %s", args[i])
		}
		i++
	}
	f, err := b.Ray.Foton(step, bindings, nil)
	if err != nil {
		fail(1, "%v", err)
	}
	type file struct {
		Path string `json:"path"`
		Hash string `json:"hash"`
	}
	spec := map[string]any{"predicate": "foton",
		"protocol": map[string]any{"kind": f.Protocol.Kind, "descriptor": f.Protocol.Descriptor}}
	var ins, outs []file
	for _, in := range f.Inputs {
		ins = append(ins, file{in.Path, in.Hash})
	}
	for _, o := range f.Outputs {
		h := ""
		if p, ok := outputs[o.Path]; ok {
			body, err := os.ReadFile(p)
			if err != nil {
				fail(1, "%v", err)
			}
			h = ktonpkg.HashOf(body)
		}
		outs = append(outs, file{o.Path, h})
	}
	spec["inputs"], spec["outputs"] = ins, outs
	emit(spec)
}

func inspect(b *ktonpkg.Bundle) {
	out := map[string]any{"kind": b.Package.Kind, "name": b.Package.Name, "format": b.Package.FormatVersion}
	if id, err := b.ID(); err == nil {
		out["bundleId"] = id
	} else {
		out["bundleIdError"] = err.Error()
	}
	var problems []string
	for _, p := range b.Verify() {
		problems = append(problems, p.String())
	}
	out["problems"] = problems
	out["verified"] = len(problems) == 0
	if b.Ray != nil {
		if id, err := b.Ray.ID(); err == nil {
			out["rayId"] = id
		}
		out["holes"] = b.Ray.Holes
		out["steps"] = len(b.Ray.Steps)
	}
	if b.Spectrum != nil {
		id, err := b.Spectrum.ID()
		if err != nil {
			fail(1, "Spectrum-Kennung: %v", err)
		}
		out["spectrumId"] = id
		out["spectrum"] = b.Spectrum
	}
	out["requires"] = b.Requires
	emit(out)
}

// stepPlan ist ein Step, so wie die git-Seite ihn an `cockpit publish` übergibt.
type stepPlan struct {
	ID      string            `json:"id"`
	Dir     string            `json:"dir"`
	Stage   []stage           `json:"stage"`
	Params  map[string]string `json:"params,omitempty"`
	Cmd     string            `json:"cmd"`
	Inputs  []string          `json:"inputs"`
	Outputs []string          `json:"outputs"`
}

// stage ist eine Kopie, die vor dem Lauf gemacht wird: die Bytes landen unter ihrem Slotnamen im
// Stepordner, weil die Skripte der Raute in ihrem Arbeitsordner lesen und schreiben. Eine Kopie
// ändert keinen Hash — darum verbindet sich die Linie über die Steps trotzdem.
type stage struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func plan(b *ktonpkg.Bundle, work string, args []string) {
	if b.Ray == nil {
		fail(1, "%s ist kein Workflow-Paket", b.Package.Name)
	}
	bindings := ktonpkg.Bindings{}
	noDefaults := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--no-defaults":
			noDefaults = true
		case "--bind":
			i++
			if i >= len(args) || !strings.Contains(args[i], "=") {
				fail(1, "--bind erwartet name=wert")
			}
			k, v, _ := strings.Cut(args[i], "=")
			bindings[k] = v
		default:
			fail(1, "unbekannt: %s", args[i])
		}
	}

	// Offene Pflichtlöcher. Mit --no-defaults zählt der mitgelieferte Testdatensatz nicht als
	// Bindung: genau das ist der Fall "unvollständig gebunden", der nicht anlaufen darf.
	var missing []string
	if noDefaults {
		for _, h := range b.Ray.Holes {
			if h.Required && bindings[h.Name] == "" {
				missing = append(missing, h.Name)
			}
		}
		sort.Strings(missing)
	} else {
		missing = b.Ray.MissingBindings(bindings)
		bindings = b.Ray.WithDefaults(bindings)
	}
	if len(missing) > 0 {
		emit(map[string]any{"refused": true, "missing": missing,
			"reason": "offene Pflichtlöcher: " + strings.Join(missing, ", ")})
		os.Exit(2)
	}

	outputs, err := declaredOutputs(b)
	if err != nil {
		fail(1, "%v", err)
	}
	order, err := b.Ray.Order()
	if err != nil {
		fail(1, "%v", err)
	}
	var steps []stepPlan
	for _, id := range order {
		s := b.Ray.Step(id)
		dir := path.Join(work, id)
		p := stepPlan{ID: id, Dir: dir}
		for _, in := range s.Inputs {
			var from string
			switch {
			case in.Pinned():
				from = path.Join(b.Dir, in.Path)
			case strings.HasPrefix(in.From, ktonpkg.FromHole):
				name := strings.TrimPrefix(in.From, ktonpkg.FromHole)
				v := bindings[name]
				if h := b.Ray.Hole(name); h != nil && v == h.Default {
					v = path.Join(b.Dir, v) // der Testdatensatz liegt im Paket
				}
				from = v
			default:
				up, slot, ok := ktonpkg.ParseUpstream(in.From)
				if !ok {
					fail(1, "%s: unlesbare Verdrahtung %q", id, in.From)
				}
				from = path.Join(work, up, slot)
			}
			to := path.Join(dir, strings.TrimPrefix(in.Slot, "./"))
			p.Stage = append(p.Stage, stage{From: from, To: to})
			p.Inputs = append(p.Inputs, to)
		}
		params, unbound := ktonpkg.ResolveParams(s, bindings)
		if len(unbound) > 0 {
			fail(2, "%s: ungebundene Parameter %v", id, unbound)
		}
		p.Params = params
		p.Cmd = "cd " + dir + " && " + commandLine(s, params)
		for _, o := range outputs[id] {
			p.Outputs = append(p.Outputs, path.Join(dir, o))
		}
		if len(p.Outputs) == 0 {
			fail(1, "%s: das Paket sagt nicht, was dieser Step erzeugt", id)
		}
		steps = append(steps, p)
	}
	emit(map[string]any{"bindings": bindings, "steps": steps})
}

// commandLine macht aus der improve-Befehlszeile eine Shell-Zeile. In improve ist die erste Zeile das
// Image (die Toolinstanz) und `<command-file>` setzt der Server; hier läuft der Befehl im Image, das
// die Cockpit-Konfiguration pinnt, und das Skript heißt wie sein Slot.
//
// Ein Paket, das aus Ausführungen extrahiert wurde, trägt die Befehlszeile so, wie sie lief — ohne
// Image-Zeile und mit dem Interpreter vorne. Die läuft, wie sie dasteht.
func commandLine(s *ktonpkg.Step, params map[string]string) string {
	lines := strings.Split(ktonpkg.ResolvedArgs(s, params), "\n")
	improve := len(lines) > 0 && strings.Contains(lines[0], "@sha256:")
	if improve {
		lines = lines[1:]
	}
	for i, l := range lines {
		if l == "<command-file>" {
			lines[i] = ktonpkg.CommandSlot(s)
		}
	}
	if improve {
		return "Rscript " + strings.Join(lines, " ")
	}
	return strings.Join(lines, " ")
}

// declaredOutputs liest, was jeder Step erzeugt, aus dem Spectrum des Pakets: ein Member je Step, die
// Referenzbytes unter reference/<step>-<datei>. Das Paket deklariert seine Ausgaben nicht — es hat
// gelaufen, und das Spectrum hält fest, was dabei herauskam.
func declaredOutputs(b *ktonpkg.Bundle) (map[string][]string, error) {
	names, err := b.List(ktonpkg.ReferenceDir)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, s := range b.Ray.Steps {
		for _, n := range names {
			base := path.Base(n)
			if strings.HasPrefix(base, s.ID+"-") {
				out[s.ID] = append(out[s.ID], strings.TrimPrefix(base, s.ID+"-"))
			}
		}
	}
	return out, nil
}

func emit(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func fail(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}
