package oxigraph

import "context"

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
