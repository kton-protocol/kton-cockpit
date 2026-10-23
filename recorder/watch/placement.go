package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Where a story's sources reside. A file the recording did not see being made came from
// somewhere: if its bytes are a foton a federated, trusted repository published, the claims about
// that foton sit in a scope, and the scope's seed names its parent. That chain is the context
// (study → project → programme in the programme example). It is read, never guessed: a source
// nobody published is "not placed".
//
// The chain is walked by asking the cockpit in the story's repository and in each federated dir
// source in turn, because `cockpit ask scope` answers only for seeds in the repository it runs in.

// Level is one scope on the way up.
type Level struct {
	Name   string `json:"name"`   // the seed's name: "study-thp-101"
	Scope  string `json:"scope"`  // the seed's id
	Source string `json:"source"` // where the seed was found: a federation source name, or "self"
}

// Placement is where one source of a story resides.
type Placement struct {
	Path        string  `json:"path"` // project-relative
	SHA256      string  `json:"sha256"`
	Placed      bool    `json:"placed"`
	Foton       string  `json:"foton,omitempty"`        // the record whose output these bytes are
	PublishedBy string  `json:"published_by,omitempty"` // the trust tier that verified it
	Levels      []Level `json:"levels,omitempty"`       // innermost first
	Note        string  `json:"note,omitempty"`         // why not, or where the chain stops
}

// Asker asks the cockpit in dir and returns its JSON answer.
type Asker func(dir string, q map[string]string) ([]byte, error)

// CockpitAsk runs `cockpit ask` (KTON_COCKPIT, else ~/.local/bin/cockpit, else cockpit on PATH).
func CockpitAsk(dir string, q map[string]string) ([]byte, error) {
	exe := os.Getenv("KTON_COCKPIT")
	if exe == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if _, err := os.Stat(filepath.Join(home, ".local", "bin", "cockpit")); err == nil {
				exe = filepath.Join(home, ".local", "bin", "cockpit")
			}
		}
	}
	if exe == "" {
		exe = "cockpit"
	}
	b, _ := json.Marshal(q)
	cmd := exec.Command(exe, "ask", string(b))
	cmd.Dir = dir
	return cmd.Output()
}

type seal struct {
	Name   string `json:"name"`
	Parent string `json:"parent"`
	Source string `json:"-"`
}

// Placer finds placements; seeds never change, so what it learnt about one is kept.
type Placer struct {
	Ask   Asker
	mu    sync.Mutex
	seals map[string]*seal // scope id → seal (nil: looked for, not found)
}

func NewPlacer(ask Asker) *Placer { return &Placer{Ask: ask, seals: map[string]*seal{}} }

// dirSources are the repository itself and the federation sources of kind dir, in config order.
func dirSources(repo string) [][2]string {
	out := [][2]string{{"self", repo}}
	b, err := os.ReadFile(filepath.Join(repo, "cockpit.config.json"))
	if err != nil {
		return out
	}
	var c struct {
		Federation struct {
			Sources []struct{ Kind, Name, Path string } `json:"sources"`
		} `json:"federation"`
	}
	if json.Unmarshal(b, &c) != nil {
		return out
	}
	for _, s := range c.Federation.Sources {
		if s.Kind == "dir" && s.Path != "" {
			p := s.Path
			if !filepath.IsAbs(p) {
				p = filepath.Join(repo, p)
			}
			out = append(out, [2]string{s.Name, p})
		}
	}
	return out
}

func (pl *Placer) sealOf(srcs [][2]string, scope string) *seal {
	pl.mu.Lock()
	s, known := pl.seals[scope]
	pl.mu.Unlock()
	if known {
		return s
	}
	for _, src := range srcs {
		b, err := pl.Ask(src[1], map[string]string{"query": "scope", "ref": scope})
		if err != nil {
			continue
		}
		var a struct{ Seal *seal }
		if json.Unmarshal(b, &a) == nil && a.Seal != nil && a.Seal.Name != "" {
			s = &seal{Name: a.Seal.Name, Parent: a.Seal.Parent, Source: src[0]}
			break
		}
	}
	pl.mu.Lock()
	pl.seals[scope] = s
	pl.mu.Unlock()
	return s
}

// Place finds where the file with these bytes resides, asked from repo.
func (pl *Placer) Place(repo, path, sum string) Placement {
	p := Placement{Path: path, SHA256: sum}
	if _, err := os.Stat(filepath.Join(repo, "cockpit.config.json")); err != nil {
		p.Note = "not a cockpit repository"
		return p
	}
	b, err := pl.Ask(repo, map[string]string{"query": "producer", "ref": "sha256:" + sum})
	if err != nil {
		p.Note = "the cockpit did not answer"
		return p
	}
	var prod struct {
		Records []struct {
			ID, Tier string
			Verified bool
		}
	}
	json.Unmarshal(b, &prod)
	for _, r := range prod.Records {
		if r.Verified {
			p.Foton, p.PublishedBy = r.ID, r.Tier
			break
		}
	}
	if p.Foton == "" {
		p.Note = "not published in any federated, trusted repository"
		return p
	}
	p.Placed = true
	b, err = pl.Ask(repo, map[string]string{"query": "about", "ref": p.Foton})
	var about struct {
		Included []string
		Claims   []struct{ ID, Scope string }
	}
	if err == nil {
		json.Unmarshal(b, &about)
	}
	ok := map[string]bool{}
	for _, id := range about.Included {
		ok[id] = true
	}
	srcs := dirSources(repo)
	for _, c := range about.Claims {
		if !ok[c.ID] || c.Scope == "" {
			continue
		}
		seen := map[string]bool{}
		for scope := c.Scope; scope != "" && !seen[scope] && len(seen) < 12; {
			seen[scope] = true
			s := pl.sealOf(srcs, scope)
			if s == nil {
				p.Note = fmt.Sprintf("the chain stops at scope %s: no federated source holds its seed", short(strings.TrimPrefix(scope, "sha256:")))
				break
			}
			p.Levels = append(p.Levels, Level{Name: s.Name, Scope: scope, Source: s.Source})
			scope = s.Parent
		}
		if len(p.Levels) > 0 {
			break
		}
	}
	if len(p.Levels) == 0 && p.Note == "" {
		p.Note = "published by " + p.PublishedBy + ", but no verified claim about it names a scope"
	}
	return p
}

// placeSources places a story's sources from the project and from outside it, the placed ones
// first; files that are gone or were never hashed are left out.
func (pl *Placer) placeSources(st *Story, project string, srcs []*Input) {
	if st == nil || pl == nil {
		return
	}
	start := time.Now()
	for _, in := range srcs {
		if in.SHA256 == "" || in.Status == "gone" || (in.Class != ClassProject && in.Class != ClassOutside) {
			continue
		}
		if len(st.Context) >= 30 || time.Since(start) > 10*time.Second {
			break
		}
		rel := in.Path
		if r, err := filepath.Rel(project, in.Path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
		st.Context = append(st.Context, pl.Place(st.Repo, rel, in.SHA256))
	}
	sort.SliceStable(st.Context, func(i, j int) bool { return len(st.Context[i].Levels) > 0 && len(st.Context[j].Levels) == 0 })
}
