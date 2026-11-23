package watch

import (
	"path/filepath"
	"testing"
)

func TestValues_HashesAndStatesFromVARS(t *testing.T) {
	l := (&logb{}).
		ev(100, "VARS", "-", "-", "d=data.frame:3x2|md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa;big=numeric:1e7|skip:size 76.3 MB").
		task(1, "ok", "d <- read.csv('pk.csv'); big <- runif(1e7)").
		ev(100, "VARS", "-", "-", "d=data.frame:3x2|md5:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb;fit=nls:|md5:cccccccccccccccccccccccccccccccc").
		task(2, "ok", "d$c <- d$c * 2; fit <- nls(...)").
		ev(100, "VARS", "-", "-", "d=data.frame:3x2|md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa;old=numeric:1").
		task(3, "ok", "d <- read.csv('pk.csv'); old <- 1")
	s := sess(t, l, nil)
	v1 := task(t, s, "1").Vars
	if v1[0].Hash != "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || v1[0].Desc != "data.frame:3x2" || v1[0].State != "new" {
		t.Fatalf("task 1 d: %+v", v1[0])
	}
	if v1[1].Skip != "size 76.3 MB" || v1[1].Hash != "" || v1[1].State != "" {
		t.Fatalf("task 1 big: %+v", v1[1])
	}
	v2 := task(t, s, "2").Vars
	if v2[0].State != "changed" || v2[1].State != "new" {
		t.Fatalf("task 2: %+v", v2)
	}
	v3 := task(t, s, "3").Vars
	if v3[0].State != "same" { // the value d had before, read again
		t.Fatalf("task 3 d: %+v", v3[0])
	}
	if v3[1].Hash != "" || v3[1].Desc != "numeric:1" || v3[1].State != "" { // an older ktonrec: no hash
		t.Fatalf("task 3 old: %+v", v3[1])
	}
}

func TestValues_SavedStore(t *testing.T) {
	dir := t.TempDir()
	v := OpenValues(dir)
	for _, bad := range []SavedValue{
		{Hash: "md5:xyz", Name: "d", Path: "/p/d.rds", Project: "/p"},
		{Hash: "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "d", Path: "d.rds", Project: "/p"},
		{Hash: "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Path: "/p/d.rds", Project: "/p"},
	} {
		if v.MarkSaved(bad) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	ok := SavedValue{Hash: "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "d", Path: "/p/objects/d-aaaaaaaa.rds", Project: "/p"}
	if err := v.MarkSaved(ok); err != nil {
		t.Fatal(err)
	}
	if x, found := OpenValues(dir).Saved(ok.Hash); !found || x.Path != ok.Path || filepath.Base(x.Path) != "d-aaaaaaaa.rds" {
		t.Fatalf("reread %+v %v", x, found)
	}
}
