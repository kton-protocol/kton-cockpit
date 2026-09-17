package container

import (
	"strings"
	"testing"
)

// What a run prints goes to the model, so it is capped on the way in rather than trimmed after —
// otherwise a command that prints a large file has already been read into memory whole.
func TestCappedBuffer_KeepsThePrefixAndSaysItTruncated(t *testing.T) {
	b := &cappedBuffer{limit: 10}
	n, err := b.Write([]byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	// The writer is told everything was accepted: a command is not failed for printing too much.
	if n != 16 {
		t.Fatalf("Write reported %d, which would make the command see a short write", n)
	}
	if got := b.String(); !strings.HasPrefix(got, "0123456789") || !strings.Contains(got, "truncated at 10") {
		t.Fatalf("got %q", got)
	}
	if b.Buffer.Len() != 10 {
		t.Fatalf("kept %d bytes, not the cap", b.Buffer.Len())
	}
}

// Output that fits says nothing about truncation, so the notice means something when it appears.
func TestCappedBuffer_SaysNothingWhenItFits(t *testing.T) {
	b := &cappedBuffer{limit: 1024}
	b.Write([]byte("qc: 113 kept, 7 dropped"))
	if got := b.String(); got != "qc: 113 kept, 7 dropped" {
		t.Fatalf("got %q", got)
	}
}

// Repeated writes accumulate against one cap, which is how a real command's output arrives.
func TestCappedBuffer_CapsAcrossManyWrites(t *testing.T) {
	b := &cappedBuffer{limit: 8}
	for i := 0; i < 100; i++ {
		b.Write([]byte("xxxx"))
	}
	if b.Buffer.Len() != 8 {
		t.Fatalf("accumulated %d bytes past a cap of 8", b.Buffer.Len())
	}
	if !strings.Contains(b.String(), "truncated") {
		t.Fatal("no truncation reported")
	}
}
