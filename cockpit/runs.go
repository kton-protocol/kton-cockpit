package cockpit

import (
	"context"
	"encoding/json"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/jobs"
)

// JobsWriter is the name the cockpit writes its runs under in the checkout's jobs journal; the
// machine's jobs runner writes under its own (ADR-016).
const JobsWriter = "cockpit"

// maxOrigin bounds a run's origin: it is a frontend's pointer to where the run was started, not data.
const maxOrigin = 4 << 10

func checkOrigin(o json.RawMessage) error {
	if len(o) == 0 {
		return nil
	}
	if len(o) > maxOrigin {
		return refuse("argument", "SPEC §12.5", "origin is %d bytes; at most %d", len(o), maxOrigin)
	}
	var m map[string]any
	if json.Unmarshal(o, &m) != nil {
		return refuse("argument", "SPEC §12.5", "origin is one JSON object")
	}
	return nil
}

// RunListRequest filters the runs: by origin (an act filter is a prefix of package, composition, part,
// act, so a subtree is one filter), and at most Limit, newest first (0: 100).
type RunListRequest struct {
	Origin json.RawMessage `json:"origin,omitempty"`
	Limit  int             `json:"limit,omitempty"`
}

// RunList is the runs of the checkout's jobs journal, every writer's.
type RunList struct {
	Runs []jobs.Run `json:"runs"`
}

// RunList lists the runs of this checkout, newest first.
func (c *Cockpit) RunList(ctx context.Context, in RunListRequest) (*RunList, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	if err := jobs.CheckActFilter(in.Origin); err != nil {
		return nil, refuse("argument", "SPEC §12.5", "%v", err)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	all, err := jobs.Runs(jobs.Dir(cfg.LocalDir))
	if err != nil {
		return nil, refuse("io", "", "the jobs journal: %v", err)
	}
	out := &RunList{Runs: []jobs.Run{}}
	for _, r := range all {
		if len(out.Runs) == limit {
			break
		}
		if jobs.Matches(r.Origin, in.Origin) {
			out.Runs = append(out.Runs, r)
		}
	}
	return out, nil
}

// RunStatus is one run by its id.
func (c *Cockpit) RunStatus(ctx context.Context, id string) (*jobs.Run, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return nil, refuse("binding", "SPEC §5", "%v", err)
	}
	all, err := jobs.Runs(jobs.Dir(cfg.LocalDir))
	if err != nil {
		return nil, refuse("io", "", "the jobs journal: %v", err)
	}
	for _, r := range all {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, refuse("run.unknown", "SPEC §12.5", "no run %q in this checkout's jobs journal", id)
}

// QueueRun records a run as queued before it starts, so its id is answered at once and run/status
// sees it (serve's workflow/run).
func (c *Cockpit) QueueRun(ctx context.Context, in WorkflowRunRequest) (string, error) {
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return "", refuse("binding", "SPEC §5", "%v", err)
	}
	if err := checkOrigin(in.Origin); err != nil {
		return "", err
	}
	id := jobs.NewID()
	if err := jobs.Append(jobs.Dir(cfg.LocalDir), JobsWriter, jobs.Line{ID: id, Kind: jobs.KindWorkflow, Origin: in.Origin, State: jobs.Queued}); err != nil {
		return "", refuse("io", "", "the jobs journal: %v", err)
	}
	return id, nil
}

// EndRun records state for a run that has not ended yet (queued or running), with why: a run refused
// before it ran, or one whose connection ended under it. A run that ended is left as it ended.
func (c *Cockpit) EndRun(ctx context.Context, id, state, why string) error {
	r, err := c.RunStatus(ctx, id)
	if err != nil || jobs.Terminal(r.State) {
		return err
	}
	cfg, err := config.Load(ctx, c.dir())
	if err != nil {
		return err
	}
	return jobs.Append(jobs.Dir(cfg.LocalDir), JobsWriter, jobs.Line{ID: id, Kind: r.Kind, State: state, Error: why})
}
