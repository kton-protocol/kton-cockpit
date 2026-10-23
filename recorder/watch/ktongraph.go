package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

// The observations in kton-web's graph form (kton-web/wasm/kernel/graph.go), so that kton-web's
// own graph viewer draws and navigates them, unchanged: a step is a "foton" node with its input
// and output files, a file nothing recorded produced is a "source" node (dataset or code), and
// "builds-on" points from the later step to what it built on. Nothing here is signed:
// every node is unverified, and says so in the viewer.

type kgRef struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
}

type kgNode struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Type        string  `json:"type"` // foton | source
	Signer      string  `json:"signer"`
	Participant string  `json:"participant"`
	Role        string  `json:"role"`
	Kind        string  `json:"kind,omitempty"` // source: dataset | code
	Headline    string  `json:"headline"`
	Text        string  `json:"text"`
	Inputs      []kgRef `json:"inputs,omitempty"`
	Outputs     []kgRef `json:"outputs,omitempty"`
	Verified    bool    `json:"verified"`
}

type kgEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type kgPart struct {
	Name     string `json:"name"`
	Fotons   int    `json:"fotons"`
	Claims   int    `json:"claims"`
	Verified int    `json:"verified"`
	Total    int    `json:"total"`
}

type kgPartEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type kgSummary struct {
	Totals struct {
		Participants int `json:"participants"`
		Fotons       int `json:"fotons"`
		Claims       int `json:"claims"`
		Verified     int `json:"verified"`
		Unverified   int `json:"unverified"`
	} `json:"totals"`
	Participants []kgPart     `json:"participants"`
	Edges        []kgPartEdge `json:"edges"`
}

type kgGraph struct {
	Nodes    []kgNode            `json:"nodes"`
	Edges    []kgEdge            `json:"edges"`
	Locators map[string][]string `json:"locators"`
	Summary  kgSummary           `json:"summary"`
	Stats    struct {
		Fotons     int `json:"fotons"`
		Claims     int `json:"claims"`
		Verified   int `json:"verified"`
		Unverified int `json:"unverified"`
	} `json:"stats"`
}

// KtonGraphDoc is what kton-web's ?graph= entry reads.
type KtonGraphDoc struct {
	Graph kgGraph `json:"graph"`
}

const kgShort = 32 // kton-web's node-identity width (graph.go shortLen)

func kgHash(h string) string {
	if len(h) > kgShort {
		return h[:kgShort]
	}
	return h
}

// StepNodeID is the graph id of one observed step.
func StepNodeID(session, task string) string {
	s := sha256.Sum256([]byte("step\x00" + session + "\x00" + task))
	return hex.EncodeToString(s[:])[:kgShort]
}

// participant: who/what did it, the way kton-web groups: a script, or the console of a session
func participant(t *Task) string {
	if t.Script != "" {
		return filepath.Base(t.Script)
	}
	if o := t.Origin(); o != nil && o.File != "" {
		return filepath.Base(o.File)
	}
	for _, s := range t.Sources {
		if s.Class == ClassProject {
			return filepath.Base(s.Path)
		}
	}
	return "console " + shortSession(t.Session)
}

func shortSession(id string) string {
	if i := strings.LastIndexByte(id, '-'); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

func stepLabel(t *Task) string {
	c := firstLine(t.Code())
	if c == "" {
		c = "command " + t.ID
	}
	return c
}

// KtonGraph builds the graph form of a project's observations. blob, when set, makes file
// bytes previewable: it returns the URL kton-web may fetch for a file version.
func (sv *Service) KtonGraph(project string, blob func(path, sum string) string) KtonGraphDoc {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	return ktonGraphOf(sv.projectTasks(NewChecker(), project), project, blob)
}

func ktonGraphOf(tasks []*Task, project string, blob func(path, sum string) string) KtonGraphDoc {
	rel := func(p string) string {
		if r, err := filepath.Rel(project, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
		return p
	}
	var g kgGraph
	g.Locators = map[string][]string{}
	sources := map[string]bool{}
	parts := map[string]*kgPart{}
	partOf := map[string]string{} // node id → participant
	pedges := map[[3]string]int{}
	edges := map[kgEdge]bool{}
	addEdge := func(e kgEdge) {
		if !edges[e] {
			edges[e] = true
			g.Edges = append(g.Edges, e)
		}
	}
	locate := func(path, sum string) {
		if blob != nil && sum != "" {
			k := kgHash(sum)
			if len(g.Locators[k]) == 0 {
				g.Locators[k] = []string{blob(path, sum)}
			}
		}
	}
	for _, t := range tasks {
		id := StepNodeID(t.Session, t.ID)
		p := participant(t)
		partOf[id] = p
		if parts[p] == nil {
			parts[p] = &kgPart{Name: p}
		}
		parts[p].Fotons++
		parts[p].Total++
		n := kgNode{ID: id, Label: stepLabel(t), Type: "foton", Participant: p, Role: stepLabel(t), Text: t.Code()}
		for _, in := range t.Inputs {
			n.Inputs = append(n.Inputs, kgRef{Hash: kgHash(in.SHA256), Name: rel(in.Path)})
			locate(in.Path, in.SHA256)
			if len(in.From) > 0 {
				for _, f := range in.From {
					s := f.Session
					if s == "" {
						s = t.Session
					}
					addEdge(kgEdge{From: id, To: StepNodeID(s, f.Task), Kind: "builds-on"})
				}
				continue
			}
			src := kgHash(in.SHA256)
			if src == "" {
				continue
			}
			if !sources[src] {
				sources[src] = true
				kind := "dataset"
				if isCodeFile(in.Path) {
					kind = "code"
				}
				g.Nodes = append(g.Nodes, kgNode{ID: src, Type: "source", Kind: kind, Role: rel(in.Path), Participant: p})
			}
			addEdge(kgEdge{From: id, To: src, Kind: "builds-on"})
		}
		for _, u := range t.Uses { // objects in the R session: the step built on the one that made them
			addEdge(kgEdge{From: id, To: StepNodeID(t.Session, u.Task), Kind: "builds-on"})
		}
		for _, o := range t.Outputs {
			n.Outputs = append(n.Outputs, kgRef{Hash: kgHash(o.SHA256), Name: rel(o.Path)})
			locate(o.Path, o.SHA256)
		}
		g.Nodes = append(g.Nodes, n)
		g.Stats.Fotons++
		g.Stats.Unverified++
	}
	for _, e := range g.Edges {
		pf, pt := partOf[e.From], partOf[e.To]
		if pf != "" && pt != "" && pf != pt {
			pedges[[3]string{pf, pt, e.Kind}]++
		}
	}
	for _, p := range parts {
		g.Summary.Participants = append(g.Summary.Participants, *p)
	}
	sort.Slice(g.Summary.Participants, func(i, j int) bool { return g.Summary.Participants[i].Name < g.Summary.Participants[j].Name })
	for k, c := range pedges {
		g.Summary.Edges = append(g.Summary.Edges, kgPartEdge{From: k[0], To: k[1], Kind: k[2], Count: c})
	}
	sort.Slice(g.Summary.Edges, func(i, j int) bool {
		return g.Summary.Edges[i].From+g.Summary.Edges[i].To < g.Summary.Edges[j].From+g.Summary.Edges[j].To
	})
	g.Summary.Totals.Participants = len(parts)
	g.Summary.Totals.Fotons = g.Stats.Fotons
	g.Summary.Totals.Unverified = g.Stats.Unverified
	return KtonGraphDoc{Graph: g}
}

func isCodeFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".r", ".py", ".qmd", ".rmd", ".ctl", ".mod", ".yaml", ".yml", ".sh", ".toml", ".jl":
		return true
	}
	return false
}

// BlobURL is the viewer-facing URL of one file version's bytes.
func BlobURL(prefix, project, path, sum string) string {
	return prefix + "?" + url.Values{"project": {project}, "path": {path}, "sha": {sum}}.Encode()
}

// KnownFile: the path and hash appear among the project's observations. The blob endpoint serves
// nothing else, so it cannot be used to read arbitrary files.
func (sv *Service) KnownFile(project, path, sum string) bool {
	sv.mu.RLock()
	defer sv.mu.RUnlock()
	for _, tr := range sv.sessions {
		if tr.s.Project != project {
			continue
		}
		for _, t := range tr.s.Tasks {
			for _, in := range t.Inputs {
				if in.Path == path && in.SHA256 == sum {
					return true
				}
			}
			for _, o := range t.Outputs {
				if o.Path == path && o.SHA256 == sum {
					return true
				}
			}
		}
	}
	return false
}
