package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"
)

// Now says how a recorded file compares with what is on disk at this moment.
const (
	NowSame    = "same"    // still there, same bytes
	NowChanged = "changed" // still there, different bytes
	NowGone    = "gone"    // no longer there
)

// Checker hashes files on demand, cached by path, size and mtime so a refresh costs a stat.
type Checker struct {
	mu    sync.Mutex
	cache map[string]string
}

func NewChecker() *Checker { return &Checker{cache: map[string]string{}} }

// State compares a file with the hash it had when recorded.
func (c *Checker) State(path, sum string) string {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return NowGone
	}
	key := fmt.Sprintf("%s|%d|%d", path, fi.Size(), fi.ModTime().UnixNano())
	c.mu.Lock()
	h, ok := c.cache[key]
	c.mu.Unlock()
	if !ok {
		f, err := os.Open(path)
		if err != nil {
			return NowGone
		}
		d := sha256.New()
		_, err = io.Copy(d, f)
		f.Close()
		if err != nil {
			return NowGone
		}
		h = hex.EncodeToString(d.Sum(nil))
		c.mu.Lock()
		c.cache[key] = h
		c.mu.Unlock()
	}
	if sum == "" || h != sum {
		return NowChanged
	}
	return NowSame
}

// Annotate returns copies of the tasks with Now set on every input and output. The session's
// own tasks are not changed.
func (c *Checker) Annotate(tasks []*Task) []*Task {
	out := make([]*Task, len(tasks))
	for i, t := range tasks {
		cp := *t
		cp.Inputs = make([]*Input, len(t.Inputs))
		for j, in := range t.Inputs {
			x := *in
			x.Now = c.State(in.Path, in.SHA256)
			cp.Inputs[j] = &x
		}
		cp.Outputs = make([]*Output, len(t.Outputs))
		for j, o := range t.Outputs {
			x := *o
			x.Now = c.State(o.Path, o.SHA256)
			cp.Outputs[j] = &x
		}
		out[i] = &cp
	}
	return out
}
