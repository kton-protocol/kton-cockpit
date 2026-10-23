package watch

import (
	"context"
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
	Source string `json:"source"` // the trust tier that verified the seed (older cockpits: the source it was found in)
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

// Asker asks the cockpit in dir and returns its JSON answer; it gives up when ctx ends.
type Asker func(ctx context.Context, dir string, q map[string]string) ([]byte, error)

// CockpitAsk runs `cockpit ask` (KTON_COCKPIT, else ~/.local/bin/cockpit, else cockpit on PATH).
// Concurrent asks in one federated repository need cockpit v0.3.0 (SPEC §9.8); older ones tear
// down each other's union cache.
func CockpitAsk(ctx context.Context, dir string, q map[string]string) ([]byte, error) {
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
	cmd := exec.CommandContext(ctx, exe, "ask", string(b))
	cmd.Dir = dir
	// the context kills the cockpit, not its children (git, docker); one of them holding stdout
	// open must not hold the story
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

type seal struct {
	Name   string `json:"name"`
	Parent string `json:"parent"`
	Source string `json:"-"`
}

// Placer finds placements. A seed never changes, so a found one is kept; a missing one may arrive
// with the next federation pull, so that is asked again after a while. Placements themselves are
// kept as briefly: a file may be published meanwhile.
type Placer struct {
	Ask    Asker
	Now    func() time.Time
	Keep   time.Duration // how long a missing seed and a placement are believed
	Budget time.Duration // the most a story's placements may take

	mu     sync.Mutex
	seals  map[string]*seal     // scope id → seal
	missed map[string]time.Time // scope id → when it was not found
	placed map[string]placedAt  // repo \x00 sha256 → placement
	chains map[string][]Level   // scope id → its whole, trusted chain
}

type placedAt struct {
	p  Placement
	at time.Time
}

func NewPlacer(ask Asker) *Placer {
	return &Placer{Ask: ask, Now: time.Now, Keep: time.Minute, Budget: 10 * time.Second,
		seals: map[string]*seal{}, missed: map[string]time.Time{}, placed: map[string]placedAt{}, chains: map[string][]Level{}}
}

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

func (pl *Placer) sealOf(ctx context.Context, srcs [][2]string, scope string) *seal {
	pl.mu.Lock()
	s, known := pl.seals[scope]
	at, missed := pl.missed[scope]
	pl.mu.Unlock()
	if known {
		return s
	}
	if missed && pl.Now().Sub(at) < pl.Keep {
		return nil
	}
	for _, src := range srcs {
		b, err := pl.Ask(ctx, src[1], map[string]string{"query": "scope", "ref": scope})
		if err != nil {
			continue
		}
		var a struct{ Seal *seal }
		if json.Unmarshal(b, &a) == nil && a.Seal != nil && a.Seal.Name != "" {
			s = &seal{Name: a.Seal.Name, Parent: a.Seal.Parent, Source: src[0]}
			break
		}
	}
	if ctx.Err() != nil {
		return nil // out of time: not a finding about the seed
	}
	pl.mu.Lock()
	if s != nil {
		pl.seals[scope] = s
		delete(pl.missed, scope)
	} else {
		pl.missed[scope] = pl.Now()
	}
	pl.mu.Unlock()
	return s
}

// Place finds where the file with these bytes resides, asked from repo.
func (pl *Placer) Place(ctx context.Context, repo, path, sum string) Placement {
	key := repo + "\x00" + sum
	pl.mu.Lock()
	c, ok := pl.placed[key]
	pl.mu.Unlock()
	if ok && pl.Now().Sub(c.at) < pl.Keep {
		c.p.Path = path
		return c.p
	}
	p := pl.place(ctx, repo, path, sum)
	if ctx.Err() == nil { // an answer cut short is not kept
		pl.mu.Lock()
		pl.placed[key] = placedAt{p, pl.Now()}
		pl.mu.Unlock()
	}
	return p
}

func (pl *Placer) place(ctx context.Context, repo, path, sum string) Placement {
	p := Placement{Path: path, SHA256: sum}
	if _, err := os.Stat(filepath.Join(repo, "cockpit.config.json")); err != nil {
		p.Note = "not a cockpit repository"
		return p
	}
	b, err := pl.Ask(ctx, repo, map[string]string{"query": "producer", "ref": "sha256:" + sum})
	if err != nil {
		p.Note = "the cockpit did not answer"
		if ctx.Err() != nil {
			p.Note = "the cockpit did not answer in time"
		}
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
	b, err = pl.Ask(ctx, repo, map[string]string{"query": "about", "ref": p.Foton})
	var about struct {
		Included []string
		Records  []struct{ ID, Tier string }
		Claims   []struct{ ID, Scope string }
	}
	if err == nil {
		json.Unmarshal(b, &about)
	}
	ok := map[string]bool{}
	for _, id := range about.Included {
		ok[id] = true
	}
	// the publisher's own word on where its file belongs comes first
	tier := map[string]string{}
	for _, r := range about.Records {
		tier[r.ID] = r.Tier
	}
	sort.SliceStable(about.Claims, func(i, j int) bool {
		return tier[about.Claims[i].ID] == p.PublishedBy && tier[about.Claims[j].ID] != p.PublishedBy
	})
	srcs := dirSources(repo)
	for _, c := range about.Claims {
		if !ok[c.ID] || c.Scope == "" {
			continue
		}
		if levels, note, done := pl.chainOf(ctx, repo, c.Scope); done {
			p.Levels, p.Note = levels, note
			if len(p.Levels) > 0 {
				break
			}
			continue
		}
		// a cockpit before SPEC §9.6's federated ask scope: ask each dir source in turn
		seen := map[string]bool{}
		for scope := c.Scope; scope != "" && !seen[scope] && len(seen) < 12; {
			seen[scope] = true
			s := pl.sealOf(ctx, srcs, scope)
			if s == nil {
				p.Note = fmt.Sprintf("the chain stops at scope %s: no federated source holds its seed", short(strings.TrimPrefix(scope, "sha256:")))
				if ctx.Err() != nil {
					p.Note = fmt.Sprintf("the chain stops at scope %s: the cockpit did not answer in time", short(strings.TrimPrefix(scope, "sha256:")))
				}
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

// chainOf asks the cockpit once for a scope and its parents (SPEC §9.6, kton-cockpit 40127a8): the
// seed and its context are read over the union of the repository and its federation sources, and
// verified against the repository's own trust tiers. done is false when the cockpit does not
// answer that way (an older one, or no seed anywhere); the caller then walks the sources itself.
func (pl *Placer) chainOf(ctx context.Context, repo, scope string) (levels []Level, note string, done bool) {
	pl.mu.Lock()
	c, known := pl.chains[scope]
	when, refused := pl.missed["chain:"+scope]
	pl.mu.Unlock()
	if known {
		return c, "", true
	}
	if refused && pl.Now().Sub(when) < pl.Keep {
		return nil, "", false
	}
	notFederated := func() ([]Level, string, bool) {
		if ctx.Err() == nil {
			pl.mu.Lock()
			pl.missed["chain:"+scope] = pl.Now()
			pl.mu.Unlock()
		}
		return nil, "", false
	}
	b, err := pl.Ask(ctx, repo, map[string]string{"query": "scope", "ref": scope})
	if err != nil {
		return notFederated()
	}
	var a struct {
		Seal *struct {
			Name     string
			SeedTier *string `json:"seedTier"`
			Context  []struct {
				Scope, Name, Tier string
				Missing           bool
			}
		}
	}
	if json.Unmarshal(b, &a) != nil || a.Seal == nil || a.Seal.SeedTier == nil {
		return notFederated()
	}
	at := short(strings.TrimPrefix(scope, "sha256:"))
	if *a.Seal.SeedTier == "" {
		return nil, fmt.Sprintf("scope %s: its seed is signed by no key trusted here", at), true
	}
	levels = []Level{{Name: a.Seal.Name, Scope: scope, Source: *a.Seal.SeedTier}}
	for _, l := range a.Seal.Context {
		at = short(strings.TrimPrefix(l.Scope, "sha256:"))
		switch {
		case l.Missing:
			return levels, fmt.Sprintf("the chain stops at scope %s: no federated source holds its seed", at), true
		case l.Tier == "":
			return levels, fmt.Sprintf("the chain stops at scope %s: its seed is signed by no key trusted here", at), true
		}
		levels = append(levels, Level{Name: l.Name, Scope: l.Scope, Source: l.Tier})
	}
	pl.mu.Lock() // a whole chain stays whole: seeds never change
	pl.chains[scope] = levels
	pl.mu.Unlock()
	return levels, "", true
}

// placeSources places a story's sources from the project and from outside it, the placed ones
// first; files that are gone or were never hashed are left out.
func (pl *Placer) placeSources(st *Story, project string, srcs []*Input) {
	if st == nil || pl == nil {
		return
	}
	// one budget for the whole story: a hanging cockpit is cut off, not waited for
	ctx, cancel := context.WithTimeout(context.Background(), pl.Budget)
	defer cancel()
	for _, in := range srcs {
		if in.SHA256 == "" || in.Status == "gone" || (in.Class != ClassProject && in.Class != ClassOutside) {
			continue
		}
		if len(st.Context) >= 30 || ctx.Err() != nil {
			break
		}
		rel := in.Path
		if r, err := filepath.Rel(project, in.Path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
		st.Context = append(st.Context, pl.Place(ctx, st.Repo, rel, in.SHA256))
	}
	sort.SliceStable(st.Context, func(i, j int) bool { return len(st.Context[i].Levels) > 0 && len(st.Context[j].Levels) == 0 })
}
