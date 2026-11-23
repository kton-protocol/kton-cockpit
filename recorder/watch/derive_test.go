package watch

import "testing"

// a pharmpy session in the console: read run1.ctl, add a lag time, rename, write run7.ctl
func TestModelOrigins_AWrittenModelLeadsBackToTheFileItWasReadFrom(t *testing.T) {
	r1, r7 := proj+"/model/run1.ctl", proj+"/model/run7.ctl"
	l := (&logb{}).
		ev(100, "open", "r", r1, "sha256=aaa").
		ev(100, "DERIVE", "-", r1, `{"fn":"read_model","in":[],"out":"H1","name":"run1"}`).
		task(1, "ok", "m = read_model('model/run1.ctl')").
		ev(100, "DERIVE", "-", "-", `{"fn":"add_lag_time","in":["H1"],"out":"H2","name":"run1"}`).
		ev(100, "DERIVE", "-", "-", `{"fn":"set_name","in":["H2"],"out":"H3","name":"run7"}`).
		task(2, "ok", "m = set_name(add_lag_time(m), 'run7')").
		ev(100, "DERIVE", "-", r7, `{"fn":"write_model","in":["H3"]}`).
		ev(100, "open", "w", r7, "new=1").
		ev(100, "closew", "w", r7, "sha256=bbb").
		task(3, "ok", "write_model(m, 'model/run7.ctl')").
		// a model built in the session from nothing read: no origin
		ev(100, "DERIVE", "-", "-", `{"fn":"create_basic_pk_model","in":[],"out":"H9","name":"basic"}`).
		ev(100, "DERIVE", "-", proj+"/model/basic.ctl", `{"fn":"write_model","in":["H9"]}`).
		ev(100, "open", "w", proj+"/model/basic.ctl", "new=1").
		ev(100, "closew", "w", proj+"/model/basic.ctl", "sha256=ccc").
		task(4, "ok", "write_model(create_basic_pk_model(), 'model/basic.ctl')")
	s := sess(t, l, map[string]string{r1: "aaa", r7: "bbb", proj + "/model/basic.ctl": "ccc"})
	if d := task(t, s, "2").Derives; len(d) != 2 || d[0].Fn != "add_lag_time" || d[0].In[0] != "H1" {
		t.Fatalf("derives of task 2: %+v", d)
	}
	if p := task(t, s, "1").Derives[0].Path; p != r1 {
		t.Fatalf("read_model's path: %q", p)
	}
	got := ModelOrigins(s, task(t, s, "3"), proj)
	if len(got) != 1 {
		t.Fatalf("origins %+v", got)
	}
	o := got[0]
	if o.Output != "model/run7.ctl" || o.From != "model/run1.ctl" || o.FromSHA != "aaa" || len(o.Via) != 2 || o.Via[0] != "add_lag_time" || o.Via[1] != "set_name" {
		t.Fatalf("origin %+v", o)
	}
	// kept on the task when it finished: readers that see only the tasks with files still have it
	if mo := task(t, s, "3").ModelOrigins; len(mo) != 1 || mo[0].From != o.From {
		t.Fatalf("model origins on the task: %+v", mo)
	}
	if got := ModelOrigins(s, task(t, s, "4"), proj); len(got) != 0 {
		t.Fatalf("a model built from nothing read has no origin: %+v", got)
	}
	if got := ModelOrigins(s, task(t, s, "1"), proj); len(got) != 0 {
		t.Fatalf("a task that wrote no model: %+v", got)
	}
}
