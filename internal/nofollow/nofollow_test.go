package nofollow

import (
	"os"
	"path/filepath"
	"testing"
)

// A link at the name is refused, for reading and for writing, and the file it points to is left
// as it was; a plain file opens (SPEC §10.4).
func TestOpenFile_RefusesALinkAtTheName(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if f, err := OpenFile(link, os.O_RDONLY, 0); err == nil {
		f.Close()
		t.Error("reading through a link: want refused")
	}
	if f, err := OpenFile(link, os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		f.Close()
		t.Error("writing through a link: want refused")
	}
	if b, _ := os.ReadFile(target); string(b) != "kept" {
		t.Errorf("the link's target was changed: %q", b)
	}
	f, err := OpenFile(target, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("a plain file: %v", err)
	}
	f.Close()
}
