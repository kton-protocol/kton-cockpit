package watch

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
)

// Whether kton has a story's steps, the rule the story tab follows (positron/extension.js,
// publishedSteps): a step is in kton when, for each file it wrote, a verified record has those
// bytes as output and names only inputs this step read with the same bytes. Output bytes in kton
// from other inputs are "same": the result is there, this run of it is not.

const (
	InKton     = "kton"
	SameResult = "same"
)

// RecordInputs is the input hashes a record names. Records never change, so the answer is kept.
func (pl *Placer) RecordInputs(ctx context.Context, repo, id string) (map[string]bool, error) {
	pl.mu.Lock()
	in, ok := pl.records[id]
	pl.mu.Unlock()
	if ok {
		return in, nil
	}
	b, err := pl.Ask(ctx, repo, map[string]string{"query": "record", "ref": id})
	if err != nil {
		return nil, err
	}
	var a struct {
		Record struct {
			Inputs []struct{ Hash string }
		}
	}
	json.Unmarshal(b, &a)
	in = map[string]bool{}
	for _, i := range a.Record.Inputs {
		in[strings.TrimPrefix(i.Hash, "sha256:")] = true
	}
	pl.mu.Lock()
	pl.records[id] = in
	pl.mu.Unlock()
	return in, nil
}

// StepsInKton is per step key ("session/task") InKton, SameResult, or nothing.
func (pl *Placer) StepsInKton(ctx context.Context, st *Story) map[string]string {
	out := map[string]string{}
	if pl == nil || st == nil || !isCockpitRepo(st.Repo) {
		return out
	}
	producers := map[string][]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, s := range st.Steps {
		for _, h := range s.Hashes {
			mu.Lock()
			_, seen := producers[h]
			producers[h] = nil
			mu.Unlock()
			if seen {
				continue
			}
			wg.Add(1)
			go func(h string) {
				defer wg.Done()
				b, err := pl.Ask(ctx, st.Repo, map[string]string{"query": "producer", "ref": "sha256:" + h})
				var a struct{ Included []string }
				if err == nil {
					json.Unmarshal(b, &a)
				}
				mu.Lock()
				producers[h] = a.Included
				mu.Unlock()
			}(h)
		}
	}
	wg.Wait()
	for _, s := range st.Steps {
		if len(s.Hashes) == 0 {
			continue
		}
		read := map[string]bool{}
		for _, h := range s.InHashes {
			read[h] = true
		}
		all, same := true, true
		for _, h := range s.Hashes {
			if len(producers[h]) == 0 {
				all, same = false, false
				break
			}
			matched := false
			for _, id := range producers[h] {
				in, err := pl.RecordInputs(ctx, st.Repo, id)
				if err != nil {
					continue
				}
				ok := true
				for x := range in {
					ok = ok && read[x]
				}
				if ok {
					matched = true
					break
				}
			}
			all = all && matched
		}
		switch {
		case all:
			out[s.Session+"/"+s.Task] = InKton
		case same:
			out[s.Session+"/"+s.Task] = SameResult
		}
	}
	return out
}
