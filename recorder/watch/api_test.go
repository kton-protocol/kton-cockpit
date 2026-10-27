package watch

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPI_SessionsFilteredTasksAndLineage(t *testing.T) {
	dir := t.TempDir()
	b := (&logb{}).
		ev(100, "open", "r", "/p/run1.mod", "sha256=mod").
		ev(100, "open", "w", "/p/run1.lst", "new=1").
		ev(100, "closew", "w", "/p/run1.lst", "sha256=lst").
		task(1, "ok", "nmfe").
		task(2, "ok", "base::.ark_last_value").
		ev(100, "open", "r", "/p/run1.lst", "sha256=lst").
		ev(100, "open", "w", "/p/report.html", "new=1").
		ev(100, "closew", "w", "/p/report.html", "sha256=rep").
		task(3, "ok", "render")
	os.WriteFile(filepath.Join(dir, "s1.tsv"), []byte("#ktontrace v1\thash=/p\n"+strings.Join(b.lines, "\n")+"\n"), 0o644)
	sv := &Service{Dir: dir, Store: t.TempDir(), Idle: time.Hour, NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	srv := httptest.NewServer(sv.Handler())
	defer srv.Close()

	get := func(path string, v any) int {
		r, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		if v != nil && r.StatusCode == 200 {
			json.NewDecoder(r.Body).Decode(v)
		}
		return r.StatusCode
	}
	var list []Summary
	if get("/api/sessions", &list); len(list) != 1 || list[0].ID != "s1" {
		t.Fatalf("sessions %+v", list)
	}
	var one struct {
		Total int     `json:"total"`
		Tasks []*Task `json:"tasks"`
	}
	if get("/api/sessions/s1?noise=1", &one); one.Total != 3 || len(one.Tasks) != 2 {
		t.Fatalf("noise filter: total %d shown %d", one.Total, len(one.Tasks))
	}
	var st Step
	if get("/api/lineage?session=s1&file=/p/report.html", &st); st.Task == nil || len(st.From) != 1 || st.From[0].Task.ID != "1" {
		t.Fatalf("lineage %+v", st)
	}
	if code := get("/api/sessions/nope", nil); code != 404 {
		t.Fatalf("unknown session: %d", code)
	}
	if code := get("/", nil); code != 200 {
		t.Fatalf("viewer: %d", code)
	}
}

// Review B1: /kton/blob serves exactly the bytes that hash to its sha, also after a same-size
// rewrite that kept the mtime (which a check by size and mtime does not see).
func TestAPI_BlobServesOnlyTheBytesOfItsHash(t *testing.T) {
	dir, proj := t.TempDir(), t.TempDir()
	f := filepath.Join(proj, "secret.txt")
	os.WriteFile(f, []byte("SECRET-A"), 0o644)
	sumA := hexOf("SECRET-A")
	log := "#ktontrace v2\thash=" + proj + "\n1\t100\t1\topen\tr\t" + f + "\t8\t0\t0\tsha256=" + sumA +
		"\n2\t100\t1\tTASK\tok\t1\t0\t0\t0\tread\n"
	os.WriteFile(filepath.Join(dir, "s1.tsv"), []byte(log), 0o644)
	sv := &Service{Dir: dir, Store: t.TempDir(), Idle: time.Hour, NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	srv := httptest.NewServer(sv.Handler())
	defer srv.Close()
	blob := func() (int, string) {
		r, err := srv.Client().Get(srv.URL + BlobURL("/kton/blob", proj, f, sumA))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, string(b)
	}
	if code, body := blob(); code != 200 || body != "SECRET-A" {
		t.Fatalf("observed bytes: %d %q", code, body)
	}
	st, _ := os.Stat(f)
	os.WriteFile(f, []byte("SECRET-B"), 0o644)
	os.Chtimes(f, st.ModTime(), st.ModTime())
	if code, body := blob(); code != 404 || strings.Contains(body, "SECRET-B") {
		t.Fatalf("rewritten file served under the old hash: %d %q", code, body)
	}
}

// ddmanymore: /api/calls?project= on 1,996 calls took 100 s and returned 207 MB. A call is what
// was recorded, so listing them hashes nothing on disk; a page, a brief form and the calls that
// wrote one file keep the answer small.
func TestAPI_CallsPagedBriefAndByWhatTheyWrote(t *testing.T) {
	dir, p := t.TempDir(), t.TempDir()
	b := &logb{}
	for i := 1; i <= 3; i++ {
		pid := 200 + i
		b.ev(pid, "exec", "-", p, fmt.Sprintf("Rscript extract.R m%d", i)).
			ev(pid, "open", "r", p+"/extract.R", "sha256=script").
			ev(pid, "open", "r", fmt.Sprintf("%s/m%d.mod", p, i), fmt.Sprintf("sha256=mod%d", i)).
			ev(pid, "open", "w", fmt.Sprintf("%s/m%d.json", p, i), "new=1").
			ev(pid, "closew", "w", fmt.Sprintf("%s/m%d.json", p, i), fmt.Sprintf("sha256=json%d", i)).
			task(i, "ok", fmt.Sprintf("extract %d", i))
	}
	os.WriteFile(filepath.Join(dir, "s1.tsv"), []byte("#ktontrace v1\thash="+p+"\n"+strings.Join(b.lines, "\n")+"\n"), 0o644)
	sv := &Service{Dir: dir, Store: t.TempDir(), Idle: time.Hour, NewClassifier: func() *Classifier { return &Classifier{} }}
	sv.Poll(time.Now())
	srv := httptest.NewServer(sv.Handler())
	defer srv.Close()
	get := func(q string) ([]Call, string) {
		r, err := srv.Client().Get(srv.URL + "/api/calls?project=" + url.QueryEscape(p) + q)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var cs []Call
		json.NewDecoder(r.Body).Decode(&cs)
		return cs, r.Header.Get("X-Total-Count")
	}
	all, n := get("")
	if len(all) != 3 || n != "3" || len(all[0].Reads) != 2 {
		t.Fatalf("all: %d calls, total %s: %+v", len(all), n, all)
	}
	page, n := get("&offset=1&limit=1")
	if len(page) != 1 || n != "3" || page[0].Key != all[1].Key {
		t.Fatalf("page: %+v, total %s", page, n)
	}
	if past, _ := get("&offset=9"); len(past) != 0 {
		t.Fatalf("past the end: %+v", past)
	}
	brief, _ := get("&brief=1")
	if len(brief) != 3 || brief[0].Reads != nil || len(brief[0].Writes) != 1 || brief[0].Key != all[0].Key {
		t.Fatalf("brief: %+v", brief[0])
	}
	for _, f := range []string{"json2", p + "/m2.json", "m2.json"} {
		if by, n := get("&wrote=" + url.QueryEscape(f)); len(by) != 1 || n != "1" || by[0].Writes[0].Rel != "m2.json" {
			t.Fatalf("wrote %s: %+v", f, by)
		}
	}
	if none, n := get("&wrote=nothing"); len(none) != 0 || n != "0" {
		t.Fatalf("wrote nothing: %+v", none)
	}
}
