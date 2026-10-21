package watch

import "testing"

func nmfeLike(t *testing.T) *Session {
	l := (&logb{}).
		ev(100, "open", "r", "/p/run1.mod", "sha256=mod").
		ev(100, "open", "r", "/p/THEOPP", "sha256=dat").
		ev(100, "open", "rw", "/p/FCON", "new=1").
		ev(100, "closew", "w", "/p/FCON", "sha256="+EmptySHA256).
		ev(101, "exec", "-", "/p", "rm -f FCON").
		ev(102, "exec", "-", "/p", "/opt/nm/util/NMTRAN.exe").
		ev(103, "exec", "-", "/p", "/bin/bash /opt/nm/run/nmfe75 run1.mod run1.lst").
		ev(100, "open", "w", "/p/run1.lst", "new=1").
		ev(100, "closew", "w", "/p/run1.lst", "sha256=lst").
		ev(100, "connect", "net", "inet:127.0.0.1:5", "class=loopback;rc=0").
		task(1, "ok", "system('nmfe75 run1.mod run1.lst')").
		task(2, "ok", "base::invisible(base::.ark_last_value)").
		ev(100, "open", "r", "/p/run1.lst", "sha256=lst").
		ev(100, "open", "w", "/p/report.html", "new=1").
		ev(100, "closew", "w", "/p/report.html", "sha256=rep").
		task(3, "ok", "render")
	return sess(t, l, nil)
}

func TestFilter_NoiseIsRemovedButTheRunStays(t *testing.T) {
	s := nmfeLike(t)
	all := Filter{}.View(s)
	clean := Filter{HideNoise: true}.View(s)
	if len(all) != 3 || len(clean) != 2 {
		t.Fatalf("all %d clean %d: Ark's empty marker must go, the rest stay", len(all), len(clean))
	}
	run := clean[0]
	if len(run.Outputs) != 1 || run.Outputs[0].Path != "/p/run1.lst" {
		t.Fatalf("empty FCON should be hidden: %+v", run.Outputs)
	}
	if len(run.Commands) != 2 || len(run.Network) != 0 {
		t.Fatalf("rm and loopback are noise: cmds %+v net %+v", run.Commands, run.Network)
	}
	if len(s.Tasks[0].Outputs) != 2 {
		t.Fatal("View must not change the session")
	}
}

func TestFilter_ToolAndFile(t *testing.T) {
	s := nmfeLike(t)
	if v := (Filter{Tool: "nmfe"}).View(s); len(v) != 1 || v[0].ID != "1" {
		t.Fatalf("tool nmfe: %+v", v)
	}
	if v := (Filter{File: "report"}).View(s); len(v) != 1 || v[0].ID != "3" {
		t.Fatalf("file report: %+v", v)
	}
}

func TestLineage_BackFromAReport(t *testing.T) {
	st := Lineage(nmfeLike(t), "/p/report.html")
	if st == nil || st.Task.ID != "3" || len(st.From) != 1 || st.From[0].Task.ID != "1" || st.From[0].File != "/p/run1.lst" {
		t.Fatalf("lineage %+v", st)
	}
}
