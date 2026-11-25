package dirfiles_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/dirfiles"
)

// Named reads one directory: a sibling whose name a pattern would match is never listed, which matters
// where the caller deletes what it lists (the query sweep in cockpit/dataset.go).
func TestDirfiles_NamedListsOneDirectoryByPrefixAndSuffix(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "tmp*")
	for _, p := range []string{
		filepath.Join(dir, "query-1"), filepath.Join(dir, "query-"), filepath.Join(dir, "other"),
		filepath.Join(base, "tmpX", "query-2"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := dirfiles.Named(dir, "query-", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(dir, "query-"), filepath.Join(dir, "query-1")}; !reflect.DeepEqual(got, want) {
		t.Errorf("Named: %v, want %v", got, want)
	}
	if got, err := dirfiles.Named(filepath.Join(base, "absent"), "", ".pub"); err != nil || got != nil {
		t.Errorf("a missing directory: %v, %v; want none, no error", got, err)
	}
}
