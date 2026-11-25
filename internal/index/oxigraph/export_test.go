package oxigraph

import (
	"context"
	"time"
)

// SetStageChunk sets how much one graph-store request carries, for a test; it returns the undo.
func SetStageChunk(n int) func() {
	was := stageChunk
	stageChunk = n
	return func() { stageChunk = was }
}

// Count answers a COUNT query over the store's own dataset, for a test.
func (d *Daemon) Count(ctx context.Context, q string) (int64, error) { return d.count(ctx, q) }

// Stage writes triples into graph g through the graph store, for a test.
func (d *Daemon) Stage(ctx context.Context, g string, qs []Quad) error { return d.stage(ctx, g, qs) }

// ViewIRI names slot's view, for a test.
func ViewIRI(slot string) string { return viewIRI(slot) }

// Staged is how many graphs were uploaded so far, for a test.
func Staged() int64 { return staged.Load() }

// SetDuringBuild sets what runs once a build has marked itself.
func SetDuringBuild(f func()) { duringBuild = f }

func Notes() int64 { return notes.Load() }

// SetReadClock fixes the time read notes carry; the returned func restores it.
func SetReadClock(f func() time.Time) func() {
	old := readClock
	readClock = f
	return func() { readClock = old }
}
