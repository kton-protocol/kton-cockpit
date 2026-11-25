package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/index/oxigraph"
	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

// When the index cannot be brought up to date, the indexMoved the events owe is sent at the next tick
// that can, though that tick brings no new record (kton-review E1 on #213).
func TestEvents_AnIndexMovedOwedIsSentOnceTheIndexIsBack(t *testing.T) {
	r := describesRepo(t)
	ctx := context.Background()
	c := New(Start{})
	since, err := c.EventsCursor(ctx)
	noErr(t, err)
	var got []string
	down := true
	w := &watcher{cfg: r.Config(t), cursor: since, labels: map[string]string{},
		emit: func(m string, _ any) { got = append(got, m) },
		update: func(context.Context) (string, *oxigraph.ViewState, error) {
			if down {
				return "", nil, errors.New("the index is restarting")
			}
			return "slot", nil, nil
		}}
	sayValue(t, "urn:example:x", "describes", "one")
	if err := w.tick(ctx); err == nil {
		t.Error("the failed update is reported to Watch")
	}
	if len(got) != 1 || got[0] != EventSaid {
		t.Fatalf("first tick: want said and no indexMoved, got %v", got)
	}
	down = false
	noErr(t, w.tick(ctx))
	if len(got) != 2 || got[1] != EventIndexMoved {
		t.Errorf("next tick, nothing new, the index back: want the owed indexMoved, got %v", got)
	}
	noErr(t, w.tick(ctx))
	if len(got) != 2 {
		t.Errorf("nothing owed any more: %v", got)
	}
}

// A since that is no cursor the events handed out is refused, not watched in silence (kton-review E2).
func TestEvents_ASinceThatIsNoCursorIsRefused(t *testing.T) {
	testrepo.New(t).Use(t)
	err := New(Start{}).CheckEventsCursor(context.Background(), "not-a-cursor")
	var ref *Refusal
	if !errors.As(err, &ref) || ref.Code != "events.cursor" {
		t.Errorf("want events.cursor, got %v", err)
	}
	cur, err := New(Start{}).EventsCursor(context.Background())
	noErr(t, err)
	noErr(t, New(Start{}).CheckEventsCursor(context.Background(), cur))
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// indexMoved names the view's graphs that changed since the connection's last one, by the hashes the
// view's mark graph keeps; the first, without a view to compare, says all; without a state the event
// carries no graphs at all, which keeps its old meaning (SPEC §12.5).
func TestEvents_IndexMovedNamesTheGraphsThatChanged(t *testing.T) {
	r := describesRepo(t)
	ctx := context.Background()
	c := New(Start{})
	since, err := c.EventsCursor(ctx)
	noErr(t, err)
	var moved []IndexMovedEvent
	var state *oxigraph.ViewState
	w := &watcher{cfg: r.Config(t), cursor: since, labels: map[string]string{},
		emit: func(m string, p any) {
			if m == EventIndexMoved {
				moved = append(moved, p.(IndexMovedEvent))
			}
		},
		update: func(context.Context) (string, *oxigraph.ViewState, error) { return "slot", state, nil }}
	state = &oxigraph.ViewState{Slot: "slot", Key: "k1", Graphs: map[string]string{"v": "1", "rest": "1", "gone": "1"}}
	sayValue(t, "urn:example:x", "describes", "one")
	noErr(t, w.tick(ctx))
	state = &oxigraph.ViewState{Slot: "slot", Key: "k2", Graphs: map[string]string{"v": "1", "rest": "2", "unscoped": "1"}}
	sayValue(t, "urn:example:x", "describes", "two")
	noErr(t, w.tick(ctx))
	state = nil
	sayValue(t, "urn:example:x", "describes", "three")
	noErr(t, w.tick(ctx))
	if len(moved) != 3 {
		t.Fatalf("want three indexMoved, got %+v", moved)
	}
	if m := moved[0]; !m.All || m.Key != "k1" || m.Graphs == nil {
		t.Errorf("first, nothing to compare with: want all and the key, got %+v", m)
	}
	if m := moved[1]; m.All || m.Key != "k2" || m.Graphs == nil || m.Removed == nil ||
		strings.Join(*m.Graphs, ",") != "rest,unscoped" || strings.Join(*m.Removed, ",") != "gone" {
		t.Errorf("second: want graphs rest,unscoped and removed gone, got %+v", m)
	}
	b, _ := json.Marshal(moved[2])
	if strings.Contains(string(b), `"graphs"`) || strings.Contains(string(b), `"all"`) || strings.Contains(string(b), `"key"`) {
		t.Errorf("no state: the event names no graphs, as before: %s", b)
	}
}
