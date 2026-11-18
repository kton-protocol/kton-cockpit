package main

// ktonwatch prune (MH 17.11.): the watcher's store grows without end (1.5 GB measured); -max-age only
// hides old logs. prune deletes by age and by size, and only what nothing published needs: a published
// foton carries its own inputs and outputs, so traces and observations are working material.
//
//	ktonwatch prune [-older 14d] [-keep <project dir>]… [-cas-total 512MB] [-log-max 20MB] [-archive <name>]… [-dry-run]
//
//   - traces (<dir>/*.tsv) and stored sessions (<store>/<id>.json) untouched for longer than -older, except
//     those of a project under a -keep folder (a workspace still in use or under investigation), those the
//     person kept (Keep in the session list, or published from), and those with draft notes
//   - the content store (cas/) down to -cas-total, the files written or used longest ago first
//   - ktonwatch.log above -log-max: its last part kept as ktonwatch.log.1, the log itself emptied
//   - archive/<name> only when named with -archive; prune lists what archive/ holds
//
// -dry-run lists what would go and changes nothing. serve -prune-every runs the same in the service.

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kton-protocol/kton-cockpit/recorder/watch"
)

type pruneOpts struct {
	Dir, Store, CAS, Log, Archive string
	Older                         time.Duration
	Keep                          []string
	CASTotal, LogMax              int64
	Archives                      []string
	DryRun                        bool
}

type pruneReport struct {
	Traces, Sessions, CASFiles int
	Bytes                      int64
	Kept                       int
	Lines                      []string
}

func (r *pruneReport) gone(what string, size int64, dry bool) {
	r.Bytes += size
	verb := "deleted"
	if dry {
		verb = "would delete"
	}
	r.Lines = append(r.Lines, fmt.Sprintf("%s %s (%s)", verb, what, human(size)))
}

func human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// sessionProject: where a stored session or a log ran (the stored session's project, else the log's
// first exec line's folder)
func sessionProject(store, dir, id string) string {
	if b, err := os.ReadFile(filepath.Join(store, id+".json")); err == nil {
		var s struct {
			Project string `json:"project"`
		}
		if json.Unmarshal(b, &s) == nil && s.Project != "" {
			return s.Project
		}
	}
	f, err := os.Open(filepath.Join(dir, id+".tsv"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for n := 0; sc.Scan() && n < 50; n++ {
		if c := strings.Split(sc.Text(), "\t"); len(c) > 5 && c[3] == "exec" {
			return c[5]
		}
	}
	return ""
}

func kept(project string, keep []string) bool {
	for _, k := range keep {
		k = strings.TrimRight(k, "/")
		if k != "" && (project == k || strings.HasPrefix(project, k+"/")) {
			return true
		}
	}
	return false
}

func prune(o pruneOpts) (*pruneReport, error) {
	r := &pruneReport{}
	cut := time.Now().Add(-o.Older)
	// 1 traces and their stored sessions, by age
	ids := map[string]bool{}
	if es, err := os.ReadDir(o.Dir); err == nil {
		for _, e := range es {
			if strings.HasSuffix(e.Name(), ".tsv") {
				ids[strings.TrimSuffix(e.Name(), ".tsv")] = true
			}
		}
	}
	if es, err := os.ReadDir(o.Store); err == nil {
		for _, e := range es {
			if strings.HasSuffix(e.Name(), ".json") && e.Name() != "notes.json" {
				ids[strings.TrimSuffix(e.Name(), ".json")] = true
			}
		}
	}
	// sessions the person kept, and those with draft notes, stay whatever their age (MH 18.11.)
	w := watch.OpenWatched(o.Store, "")
	notes := watch.OpenNotes(filepath.Join(o.Store, "notes")).All()
	hasNotes := func(id string) bool {
		for k := range notes {
			if strings.HasPrefix(k, id+"/") {
				return true
			}
		}
		return false
	}
	for id := range ids {
		var newest time.Time
		var size int64
		files := []string{}
		for _, p := range []string{filepath.Join(o.Dir, id+".tsv"), filepath.Join(o.Store, id+".json")} {
			if fi, err := os.Stat(p); err == nil {
				files = append(files, p)
				size += fi.Size()
				if fi.ModTime().After(newest) {
					newest = fi.ModTime()
				}
			}
		}
		if len(files) == 0 || newest.After(cut) {
			continue
		}
		if w.Kept(id) || hasNotes(id) || kept(sessionProject(o.Store, o.Dir, id), o.Keep) {
			r.Kept++
			continue
		}
		for _, p := range files {
			if !o.DryRun {
				if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
					return r, err
				}
			}
			if strings.HasSuffix(p, ".tsv") {
				r.Traces++
			} else {
				r.Sessions++
			}
		}
		r.Bytes += size
	}
	if r.Traces+r.Sessions > 0 {
		verb := "deleted"
		if o.DryRun {
			verb = "would delete"
		}
		r.Lines = append(r.Lines, fmt.Sprintf("%s %d logs and %d stored sessions untouched for %s (%s)", verb, r.Traces, r.Sessions, o.Older, human(r.Bytes)))
	}
	if r.Kept > 0 {
		r.Lines = append(r.Lines, fmt.Sprintf("kept %d old sessions (kept, with notes, or of a kept project)", r.Kept))
	}
	// 2 the content store, down to its size cap: written or used longest ago first
	if o.CASTotal > 0 && o.CAS != "" {
		type cf struct {
			p    string
			size int64
			t    time.Time
		}
		var all []cf
		var total int64
		_ = filepath.WalkDir(o.CAS, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if fi, err := d.Info(); err == nil {
				t := fi.ModTime()
				if at := atime(fi); at.After(t) {
					t = at
				}
				all = append(all, cf{p, fi.Size(), t})
				total += fi.Size()
			}
			return nil
		})
		if total > o.CASTotal {
			sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
			var freed int64
			for _, f := range all {
				if total-freed <= o.CASTotal {
					break
				}
				if !o.DryRun {
					os.Chmod(f.p, 0o644) // files in the cas are read-only
					if err := os.Remove(f.p); err != nil && !os.IsNotExist(err) {
						continue
					}
				}
				freed += f.size
				r.CASFiles++
			}
			r.gone(fmt.Sprintf("%d files from the content store, %s → %s", r.CASFiles, human(total), human(total-freed)), freed, o.DryRun)
		}
	}
	// 3 the service's own log
	if o.LogMax > 0 && o.Log != "" {
		if fi, err := os.Stat(o.Log); err == nil && fi.Size() > o.LogMax {
			if !o.DryRun {
				if err := rotateLog(o.Log, o.LogMax/4); err != nil {
					return r, err
				}
			}
			verb := "rotated"
			if o.DryRun {
				verb = "would rotate"
			}
			r.Bytes += fi.Size() - o.LogMax/4
			r.Lines = append(r.Lines, fmt.Sprintf("%s %s: its last %s kept as .1 (%s freed)", verb, filepath.Base(o.Log), human(o.LogMax/4), human(fi.Size()-o.LogMax/4)))
		}
	}
	// 4 archive/: listed; deleted only when named
	if es, err := os.ReadDir(o.Archive); err == nil {
		for _, e := range es {
			p := filepath.Join(o.Archive, e.Name())
			size := dirSize(p)
			named := false
			for _, a := range o.Archives {
				named = named || a == e.Name()
			}
			if !named {
				r.Lines = append(r.Lines, fmt.Sprintf("archive/%s: %s (kept; -archive %s deletes it)", e.Name(), human(size), e.Name()))
				continue
			}
			if !o.DryRun {
				if err := os.RemoveAll(p); err != nil {
					return r, err
				}
			}
			r.gone("archive/"+e.Name(), size, o.DryRun)
		}
	}
	return r, nil
}

// rotateLog keeps the log's last keep bytes as <log>.1 and empties the log in place: the service holds it
// open in append mode, so it goes on writing at the start
func rotateLog(log string, keep int64) error {
	f, err := os.Open(log)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, _ := f.Stat()
	if fi.Size() > keep {
		f.Seek(fi.Size()-keep, io.SeekStart)
	}
	tail, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if err := os.WriteFile(log+".1", tail, 0o644); err != nil {
		return err
	}
	return os.Truncate(log, 0)
}

func dirSize(p string) int64 {
	var n int64
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
		}
		return nil
	})
	return n
}

// sizeFlag: 512MB, 20MB, 1GB, or bytes
type sizeFlag int64

func (s *sizeFlag) String() string { return human(int64(*s)) }
func (s *sizeFlag) Set(v string) error {
	v = strings.TrimSpace(strings.ToUpper(v))
	mul := int64(1)
	for suf, m := range map[string]int64{"GB": 1 << 30, "MB": 1 << 20, "KB": 1 << 10} {
		if strings.HasSuffix(v, suf) {
			mul, v = m, strings.TrimSuffix(v, suf)
			break
		}
	}
	var n float64
	if _, err := fmt.Sscanf(v, "%g", &n); err != nil {
		return fmt.Errorf("size %q: e.g. 512MB", v)
	}
	*s = sizeFlag(n * float64(mul))
	return nil
}

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// dayDuration: 14d as well as Go's 336h
type dayDuration time.Duration

func (d *dayDuration) String() string { return time.Duration(*d).String() }
func (d *dayDuration) Set(v string) error {
	if strings.HasSuffix(v, "d") {
		var n float64
		if _, err := fmt.Sscanf(strings.TrimSuffix(v, "d"), "%g", &n); err != nil {
			return err
		}
		*d = dayDuration(time.Duration(n * float64(24*time.Hour)))
		return nil
	}
	x, err := time.ParseDuration(v)
	*d = dayDuration(x)
	return err
}

func pruneFlags(fs *flag.FlagSet) *pruneOpts {
	o := &pruneOpts{}
	older, cas, logMax := dayDuration(14*24*time.Hour), sizeFlag(512<<20), sizeFlag(20<<20)
	var keep, archives listFlag
	fs.Var(&older, "older", "delete logs and stored sessions untouched for longer than this (e.g. 14d)")
	fs.Var(&keep, "keep", "a project folder whose sessions are kept whatever their age (repeatable)")
	fs.Var(&cas, "cas-total", "the content store's size cap (0: no cap)")
	fs.Var(&logMax, "log-max", "rotate ktonwatch.log above this size (0: never)")
	fs.Var(&archives, "archive", "delete archive/<name> (repeatable); without it archive/ is only listed")
	fs.BoolVar(&o.DryRun, "dry-run", false, "list what would go, change nothing")
	o.Dir, o.Store, o.CAS = cacheDir("trace"), cacheDir("observations"), cacheDir("cas")
	o.Log, o.Archive = cacheDir("ktonwatch.log"), cacheDir("archive")
	// the values are read after Parse
	pruneParsed = func() {
		o.Older, o.Keep, o.CASTotal, o.LogMax, o.Archives = time.Duration(older), keep, int64(cas), int64(logMax), archives
	}
	return o
}

var pruneParsed func()

func pruneCmd(args []string) {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	o := pruneFlags(fs)
	fs.StringVar(&o.Dir, "dir", o.Dir, "the logs")
	fs.StringVar(&o.Store, "store", o.Store, "the stored sessions")
	fs.StringVar(&o.CAS, "cas", o.CAS, "the content store")
	fs.Parse(args)
	pruneParsed()
	r, err := prune(*o)
	for _, l := range r.Lines {
		fmt.Println(l)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ktonwatch prune:", err)
		os.Exit(1)
	}
	if len(r.Lines) == 0 {
		fmt.Println("nothing to prune")
	}
}
