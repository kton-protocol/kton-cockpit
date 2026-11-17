package watch

import "testing"

// MH 12.11., jf1-git3: git's commit (index.lock renamed to index, refs updated) is no output of a step
func TestSession_GitMetadataIsNoOutput(t *testing.T) {
	s := NewSession("terminal-1", "/log.tsv", &Classifier{System: DefaultSystem, Temp: DefaultTemp, Project: "/p"})
	s.Line("#ktontrace v3\thash=/p")
	s.Line("10\t300\t1\texec\tq\t/p\t0\t0\t0\tgit commit -m x")
	s.Line("11\t300\t1\topen\tw\t/p/.git/index.lock\t0\t1\t2\tnew=1")
	s.Line("12\t300\t1\tclosew\tw\t/p/.git/index.lock\t0\t1\t2\tsha256=aa")
	s.Line("13\t300\t1\trename\t-\t/p/.git/index\t0\t0\t0\t/p/.git/index.lock")
	s.Line("14\t300\t1\topen\tw\t/p/out.csv\t0\t1\t3\tnew=1")
	s.Line("15\t300\t1\tclosew\tw\t/p/out.csv\t0\t1\t3\tsha256=bb")
	s.Line("20\t300\t1\texit\t-\t-\t0\t0\t0\t0")
	var outs []string
	for _, tk := range s.Tasks {
		for _, o := range tk.Outputs {
			outs = append(outs, o.Path)
		}
	}
	for _, o := range outs {
		if o != "/p/out.csv" {
			t.Fatalf("outputs %v: .git is never an output", outs)
		}
	}
}

// MH 12.11.: the cockpit's own calls (publish from a typed terminal, with its git) are no part of the work
func TestSession_TheToolingIsNotRecorded(t *testing.T) {
	s := NewSession("terminal-2", "/log.tsv", &Classifier{System: DefaultSystem, Temp: DefaultTemp, Project: "/p"})
	s.Line("#ktontrace v3\thash=/p")
	s.Line("10\t400\t1\texec\tq\t/p\t0\t0\t0\tRscript script/gof.R run1")
	s.Line("11\t400\t1\topen\tw\t/p/deliv/gof.png\t0\t1\t2\tnew=1")
	s.Line("12\t400\t1\tclosew\tw\t/p/deliv/gof.png\t0\t1\t2\tsha256=aa")
	s.Line("13\t400\t1\texit\t-\t-\t0\t0\t0\t0")
	s.Line("20\t500\t1\texec\tq\t/p\t0\t0\t0\tenv KTON_OUT= /home/u/.local/bin/cockpit-improve publish {}")
	s.Line("21\t501\t500\texec\tq\t/p\t0\t0\t0\tgit add -- deliv/gof.png")
	s.Line("22\t501\t500\topen\tw\t/p/registry/x.json\t0\t1\t3\tnew=1")
	s.Line("23\t501\t500\tclosew\tw\t/p/registry/x.json\t0\t1\t3\tsha256=bb")
	s.Line("24\t500\t1\texit\t-\t-\t0\t0\t0\t0")
	for _, tk := range s.Tasks {
		for _, c := range tk.Commands {
			if isTooling(c.Cmdline) || c.PID == 501 {
				t.Fatalf("a tooling command is recorded: %q", c.Cmdline)
			}
		}
		for _, o := range tk.Outputs {
			if o.Path == "/p/registry/x.json" {
				t.Fatalf("the cockpit's write is an output")
			}
		}
	}
	if !isTooling("/usr/bin/env cockpit say {}") || isTooling("cockpit-improve-nmfe75 run1.ctl") || isTooling("nmfe75 x.ctl") {
		t.Fatalf("isTooling")
	}
}
