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
