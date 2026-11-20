package cockpit

import (
	"context"
	"errors"
	"testing"

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
		update: func(context.Context) (string, error) {
			if down {
				return "", errors.New("the index is restarting")
			}
			return "slot", nil
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
