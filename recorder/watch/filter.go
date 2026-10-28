package watch

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/toolcall"
)

// Filter narrows what is shown. The zero value shows everything.
type Filter struct {
	HideNoise  bool   // drop what the spikes showed is never of interest (see Noise)
	OnlyEffect bool   // only tasks that read or wrote project data, or reached outside
	Tool       string // only tasks that ran this program (nmfe, execute, bbi, python, quarto, Rscript …)
	File       string // only tasks whose origin, sourced file, input or output path contains this
}

// HelperPrograms are started by shells and tools as plumbing (S3: ~90 per NONMEM run).
var HelperPrograms = map[string]bool{
	"rm": true, "cp": true, "mv": true, "cat": true, "sed": true, "grep": true, "uname": true, "which": true,
	"date": true, "basename": true, "dirname": true, "mkdir": true, "ls": true, "sh": true, "bash": true,
	"env": true, "wc": true, "tr": true, "head": true, "tail": true, "cut": true, "expr": true, "readlink": true,
	"touch": true, "sleep": true, "true": true, "false": true, "test": true, "[": true, "chmod": true, "ln": true,
	"f951": true, "as": true, "x86_64-linux-gnu-as": true, "collect2": true, "ld": true, "cc1": true,
}

var interpreters = map[string]bool{"bash": true, "sh": true, "dash": true, "perl": true, "python": true, "python3": true}

// Name is the bare program name of a command. For an interpreter running a script it is the
// script: `/bin/bash …/nmfe75 run1.mod` is nmfe75, `/usr/bin/perl …/execute` is execute.
func (c Command) Name() string {
	f := toolcall.SplitCmdline(c.Cmdline)
	p := c.Program
	if p == "" && len(f) > 0 {
		p = f[0]
	}
	name := filepath.Base(p)
	if interpreters[name] || strings.HasPrefix(name, "python3.") {
		for _, a := range f[1:] {
			if a == "-c" {
				break // inline code: the interpreter itself is the program
			}
			if !strings.HasPrefix(a, "-") {
				return filepath.Base(a)
			}
		}
	}
	return name
}

// Noise reports whether a task, after noise removal, has anything left worth showing.
func (f Filter) apply(t *Task, cls *Classifier) *Task {
	if !f.HideNoise {
		return t
	}
	c := *t
	c.Inputs = nil
	for _, in := range t.Inputs {
		if in.SHA256 == EmptySHA256 || in.Rewritten || Scratch(in.Path) {
			continue // empty, rewritten by the same task, or a tool's work file
		}
		c.Inputs = append(c.Inputs, in)
	}
	c.Outputs = nil
	for _, o := range t.Outputs {
		if o.SHA256 == EmptySHA256 || Scratch(o.Path) {
			continue
		}
		c.Outputs = append(c.Outputs, o)
	}
	c.Commands = nil
	for _, cm := range t.Commands {
		if !HelperPrograms[cm.Name()] {
			c.Commands = append(c.Commands, cm)
		}
	}
	c.Network = nil
	for _, n := range t.Network {
		if n.Class == "remote" {
			c.Network = append(c.Network, n)
		}
	}
	c.Sources = nil
	for _, s := range t.Sources {
		if s.Class == ClassProject || s.Class == ClassOutside {
			c.Sources = append(c.Sources, s)
		}
	}
	c.Missing = nil
	for _, m := range t.Missing {
		if !probe(m) {
			c.Missing = append(c.Missing, m)
		}
	}
	c.Listings = nil
	c.Temporary = nil
	return &c
}

// nonmemScratch are the work files nmfe leaves in a run directory (S3): never data.
var nonmemScratch = regexp.MustCompile(`(^|/)(FCON|FSTREAM|FDATA|FDATA\.csv|FSIZES|FREPORT|FSUBS|FSUBS2|FSUBS\.f90|FSUBS_MU\.F90|FMSG|FLIB|PRSIZES\.f90|LINK\.LNK|LINKC\.LNK|compile\.lnk|nonmem|nmprd4p\.mod|OUTPUT|INTER|PRDERR|OFV\.TXT|JUNK|newline|garbage\.out|trash\.out|trash\.tmp|nmprd4p\.mod0|[^/]+\.knit\.md|[^/]+\.rmarkdown|gfortran\.txt|FILE\d+|fort\.\d+|tempzzzz\d*|temporaryfile\.xml|\w+\.set)$|(^|/)temp_dir/`)

// Scratch reports a path that is a known tool work file.
func Scratch(p string) bool { return nonmemScratch.MatchString(p) }

// Effect: the task touched project data, reached outside the project, or left a gap.
func Effect(t *Task) bool {
	for _, in := range t.Inputs {
		if in.Class == ClassProject || in.Class == ClassOutside {
			return true
		}
	}
	for _, n := range t.Network {
		if n.Class == "remote" {
			return true
		}
	}
	return len(t.Outputs) > 0 || len(t.Missing) > 0 || len(t.Unrecorded) > 0
}

func (f Filter) match(t *Task) bool {
	if f.OnlyEffect && !Effect(t) {
		return false
	}
	if f.Tool != "" {
		ok := false
		for _, c := range t.Commands {
			if strings.Contains(c.Name(), f.Tool) || strings.Contains(c.Cmdline, f.Tool) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.File != "" {
		ok := false
		for _, o := range t.Origins {
			ok = ok || strings.Contains(o.File, f.File)
		}
		for _, s := range t.Sources {
			ok = ok || strings.Contains(s.Path, f.File)
		}
		for _, in := range t.Inputs {
			ok = ok || strings.Contains(in.Path, f.File)
		}
		for _, o := range t.Outputs {
			ok = ok || strings.Contains(o.Path, f.File)
		}
		if !ok {
			return false
		}
	}
	return true
}

// empty: nothing left after noise removal (Ark's own bookkeeping markers, idle prompts)
func empty(t *Task) bool {
	return len(t.Inputs)+len(t.Outputs)+len(t.Commands)+len(t.Network)+len(t.Missing)+len(t.Unrecorded)+len(t.Origins)+len(t.Sources)+len(t.Vars) == 0
}

// View returns the tasks of a session that pass the filter, noise removed if asked. The
// session itself is not changed.
func (f Filter) View(s *Session) []*Task {
	labelled := false // the session has origins from Positron
	for _, t := range s.Tasks {
		labelled = labelled || len(t.Origins) > 0
	}
	var out []*Task
	for i, t := range s.Tasks {
		v := f.apply(t, s.cls)
		if f.HideNoise && empty(v) {
			continue
		}
		// Positron's kernel start: Ark's language server reads every R file of the workspace
		// before the first command (S8). Nothing the person did.
		if f.HideNoise && labelled && i == 0 && len(t.Origins) == 0 && len(v.Outputs) == 0 {
			continue
		}
		if f.match(v) {
			out = append(out, v)
		}
	}
	return out
}

// Step is one link of a lineage: the task that produced a file and what it read.
type Step struct {
	Session string  `json:"session,omitempty"`
	Task    *Task   `json:"task"`
	File    string  `json:"file"`
	From    []*Step `json:"from,omitempty"`
}

// Lineage walks back from a file to the tasks that produced it and their inputs, as far as
// the session recorded. The latest producer of the file is the starting point.
func Lineage(s *Session, file string) *Step {
	byID := map[string]*Task{}
	for _, t := range s.Tasks {
		byID[t.ID] = t
	}
	var producer *Task
	for _, t := range s.Tasks {
		for _, o := range t.Outputs {
			if o.Path == file {
				producer = t
			}
		}
	}
	if producer == nil {
		return nil
	}
	seen := map[string]bool{}
	var walk func(t *Task, file string) *Step
	walk = func(t *Task, file string) *Step {
		st := &Step{Task: t, File: file}
		if seen[t.ID+"\x00"+file] {
			return st
		}
		seen[t.ID+"\x00"+file] = true
		for _, in := range t.Inputs {
			for _, r := range in.From {
				if up, ok := byID[r.Task]; ok {
					st.From = append(st.From, walk(up, r.Path))
				}
			}
		}
		return st
	}
	return walk(producer, file)
}

// toolProbe: files R, NONMEM and PsN look for without needing them (S3, working day).
var toolProbe = regexp.MustCompile(`^(\.Renviron.*|\.Rprofile|\.RData|stdin|[^/]*\.so(\.\d+)*|psn\..+|(sizes|prdims|nmprd4p?|nmprd_(int|real|char)|nmbayes_(int|real)|prcom_int|procm_int)\.mod)$`)

// probe: a missing file that is a startup or tool probe, not a tried input.
func probe(p string) bool { return toolProbe.MatchString(filepath.Base(p)) || Scratch(p) }
