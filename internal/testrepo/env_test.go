package testrepo

import (
	"context"
	"strings"
	"testing"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
)

const (
	class = "sha256:" + "ab" + "cdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	img1  = "oci://example.test/nonmem@sha256:" + "1111111111111111111111111111111111111111111111111111111111111111"
	img2  = "oci://example.test/nonmem@sha256:" + "2222222222222222222222222222222222222222222222222222222222222222"
)

// work is one computation, ready to be authored in an image.
func work(t *testing.T, r *Repo) binaries.AuthorInput {
	t.Helper()
	r.Write(t, "data/in.csv", "id,value\n1,42\n")
	r.Write(t, "data/out.csv", "id,result\n1,84\n")
	return binaries.AuthorInput{Inputs: []string{"data/in.csv"}, Outputs: []string{"data/out.csv"},
		Cmd: "estimate data/in.csv > data/out.csv", SignKey: r.Config(t).PlanktonKey}
}

// Without an environment class the image is all that says what ran, so it stays in the record:
// the same computation in two images is two records (ADR-010, rule 3).
func TestAuthor_WithoutAClassTheImageIsPartOfTheRecord(t *testing.T) {
	ctx := context.Background()
	r := New(t)
	in := work(t, r)
	run := binaries.New(r.Config(t))
	in.EnvRef = img1
	a, err := run.Author(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.EnvRef = img2
	b, err := run.Author(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatalf("two images without a class gave one record %s", a.ID)
	}
	f, err := run.Foton(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Protocol.Descriptor["envRef"] != img1 {
		t.Fatalf("the image is not in the record: %v", f.Protocol.Descriptor)
	}
}

// The guards `plankton author` has hold on the cockpit's own path: a blank image, and a class that
// is not a canonical content hash ("sha256:" alone would let two images share a record), are refused.
func TestAuthor_RefusesABlankImageAndAClassThatNamesNothing(t *testing.T) {
	ctx := context.Background()
	r := New(t)
	run := binaries.New(r.Config(t))
	for name, mod := range map[string]func(*binaries.AuthorInput){
		"blank envRef":     func(in *binaries.AuthorInput) { in.EnvRef = "  " },
		"empty class":      func(in *binaries.AuthorInput) { in.Environment, in.EnvRef = "sha256:", img1 },
		"short class":      func(in *binaries.AuthorInput) { in.Environment, in.EnvRef = "sha256:abcd", img1 },
		"upper-case class": func(in *binaries.AuthorInput) { in.Environment, in.EnvRef = strings.ToUpper(class), img1 },
	} {
		in := work(t, r)
		mod(&in)
		if _, err := run.Author(ctx, in); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
