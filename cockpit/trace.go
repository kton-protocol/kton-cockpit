package cockpit

import (
	"fmt"
	"io"
	"os"
	"time"
)

// A trace of a verb's phases with their durations, on stderr, when COCKPIT_TRACE is set: where a slow say
// spends its time in a registry nobody else can measure (userinterface, T12). Off, it costs nothing.

type trace struct {
	what        string
	start, last time.Time
	w           io.Writer
}

// traceOut is where a trace goes; a test replaces it.
var traceOut io.Writer = os.Stderr

func newTrace(what string) *trace {
	if os.Getenv("COCKPIT_TRACE") == "" {
		return nil
	}
	now := time.Now()
	return &trace{what: what, start: now, last: now, w: traceOut}
}

// mark ends the phase named, since the last mark.
func (t *trace) mark(phase string) {
	if t == nil {
		return
	}
	now := time.Now()
	fmt.Fprintf(t.w, "trace %s: %-40s %8.3fs\n", t.what, phase, now.Sub(t.last).Seconds())
	t.last = now
}

func (t *trace) end() {
	if t == nil {
		return
	}
	fmt.Fprintf(t.w, "trace %s: %-40s %8.3fs\n", t.what, "total", time.Since(t.start).Seconds())
}
