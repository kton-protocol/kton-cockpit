package watch

import (
	"context"
	"encoding/json"
	"strings"
)

// Published upstream (MH 17.11., git backend): a story tells what the recorder saw in this folder.
// When a file the story starts from was made by a step recorded elsewhere (a clone, a colleague,
// an earlier iteration) and published, that step is in kton, not in the recording. Its foton says
// what ran, on what, and what came out, so the story shows it as a step of its own, before the
// recorded ones: lineage of each such file, every foton not already a recorded step.

// PublishedStep is a foton told as a story step.
type PublishedStep struct {
	Foton   string     `json:"foton"`
	Cmd     string     `json:"cmd"`
	Tier    string     `json:"tier,omitempty"`
	Inputs  []FileHash `json:"inputs"`
	Outputs []FileHash `json:"outputs"`
}

// FileHash is a path in the repository and its sha256 (bare hex).
type FileHash struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type askRec struct {
	Record *struct {
		ID      string
		Cmd     string
		Inputs  []struct{ Path, Hash string }
		Outputs []struct{ Path, Hash string }
	}
}

// addPublished puts the published steps upstream of the story's sources in front of its steps.
func (pl *Placer) addPublished(ctx context.Context, st *Story) {
	if st == nil || pl == nil || st.Repo == "" {
		return
	}
	made := map[string]bool{} // output hashes of the recorded steps
	for _, s := range st.Steps {
		for _, h := range s.Hashes {
			made[bare(h)] = true
		}
	}
	// the files to follow up: the story's sources nobody here made (and the target, when nothing here made it)
	var want []string
	for _, in := range st.sources {
		if in.SHA256 != "" && !made[in.SHA256] && (in.Class == ClassProject || in.Class == ClassOutside) {
			want = append(want, in.SHA256)
		}
	}
	if len(st.Steps) == 0 && st.Target.SHA256 != "" {
		want = append(want, st.Target.SHA256)
	}
	tier := map[string]string{}
	var ids []string
	seen := map[string]bool{}
	for _, h := range want {
		if ctx.Err() != nil {
			break
		}
		b, err := pl.Ask(ctx, st.Repo, map[string]string{"query": "lineage", "ref": "sha256:" + h})
		if err != nil {
			continue
		}
		var a struct {
			Records []struct{ ID, Tier string }
		}
		if json.Unmarshal(b, &a) != nil {
			continue
		}
		for _, r := range a.Records {
			if !seen[r.ID] {
				seen[r.ID] = true
				ids = append(ids, r.ID)
				tier[r.ID] = r.Tier
			}
		}
	}
	var steps []PublishedStep
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		b, err := pl.Ask(ctx, st.Repo, map[string]string{"query": "record", "ref": id})
		if err != nil {
			continue
		}
		var a askRec
		if json.Unmarshal(b, &a) != nil || a.Record == nil {
			continue
		}
		p := PublishedStep{Foton: id, Cmd: a.Record.Cmd, Tier: tier[id]}
		here := false
		for _, o := range a.Record.Outputs {
			p.Outputs = append(p.Outputs, FileHash{o.Path, bare(o.Hash)})
			here = here || made[bare(o.Hash)]
		}
		if here { // recorded in this folder as well: the story already tells it
			continue
		}
		for _, i := range a.Record.Inputs {
			p.Inputs = append(p.Inputs, FileHash{i.Path, bare(i.Hash)})
		}
		steps = append(steps, p)
	}
	if len(steps) == 0 {
		return
	}
	steps = upstreamFirst(steps)
	first := int64(0)
	if len(st.Steps) > 0 {
		first = st.Steps[0].When
	}
	out := make([]StoryStep, 0, len(steps)+len(st.Steps))
	for i, p := range steps {
		ss := StoryStep{Session: "kton", Task: p.Foton, When: first - int64(len(steps)-i), OnPath: true,
			Where: "$ " + p.Cmd, Sentence: "published in kton, recorded elsewhere: " + p.Cmd, Code: p.Cmd,
			Tools: toolsOfCmd(p.Cmd), Published: &steps[i]}
		for _, f := range p.Inputs {
			ss.Reads = append(ss.Reads, f.Path)
			ss.InHashes = append(ss.InHashes, f.SHA256)
		}
		for _, f := range p.Outputs {
			ss.Writes = append(ss.Writes, f.Path)
			ss.Hashes = append(ss.Hashes, f.SHA256)
		}
		out = append(out, ss)
	}
	st.Steps = append(out, st.Steps...)
}

// upstreamFirst orders fotons so a step comes after those that made its inputs (lineage lists the
// target's producer first)
func upstreamFirst(steps []PublishedStep) []PublishedStep {
	by := map[string]int{}
	for i, s := range steps {
		for _, o := range s.Outputs {
			by[o.SHA256] = i
		}
	}
	done := make([]bool, len(steps))
	var out []PublishedStep
	var visit func(i int, depth int)
	visit = func(i, depth int) {
		if done[i] || depth > len(steps) {
			return
		}
		done[i] = true
		for _, in := range steps[i].Inputs {
			if j, ok := by[in.SHA256]; ok && j != i {
				visit(j, depth+1)
			}
		}
		out = append(out, steps[i])
	}
	for i := len(steps) - 1; i >= 0; i-- {
		visit(i, 0)
	}
	return out
}

func toolsOfCmd(cmd string) []string {
	var out []string
	for _, tn := range toolNames {
		if strings.Contains(cmd, tn.match) {
			out = append(out, tn.name)
		}
	}
	return out
}

func bare(h string) string { return strings.TrimPrefix(h, "sha256:") }
