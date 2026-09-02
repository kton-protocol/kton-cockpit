package container

import (
	"io/fs"
	"path/filepath"
	"sort"
	"time"
)

// A Snapshot is what the working tree looked like at one moment, so a run can be asked what it
// changed.
//
// This exists because when the cockpit runs the command it is the only party that can hold the
// declaration against something. A publish names its outputs, and an output produced but not named
// is invisible: the foton understates what the work produced, nothing fails, and the omission
// surfaces later as a chain that does not join.
//
// It reports; it does not replace the declaration. Taking "everything that changed" as the outputs
// would put whatever a run happens to leave behind — a cache, a log, a temp file — into the
// descriptor, and output paths and hashes are COVERED: they are part of the foton's identity. Two
// otherwise identical runs would then produce different fotons, which destroys exactly the property
// the pinning and the container were for. Inputs cannot be observed at all — which files a command
// READ is not visible from outside without tracing syscalls — so an automatic answer would in any
// case be half an answer, and the missing half is the more consequential one.
type Snapshot map[string]fileState

type fileState struct {
	size int64
	mod  time.Time
	mode fs.FileMode
}

// TakeSnapshot fingerprints every file under root, except git's own bookkeeping.
//
// Size, modification time and mode rather than content hashes: the question is what a command just
// wrote, and a command that writes a file moves its mtime. Hashing a whole working tree twice per
// publish would cost real time on a repo holding data, to answer a question the fingerprint already
// answers. Nothing here is a security check — a run that deliberately restored a file's timestamps
// is not the case this is looking for.
//
// The registry is deliberately NOT excluded. The command has the repo mounted, so it could write
// there, and if it did that is worth saying out loud rather than filtering away.
func TakeSnapshot(root string) (Snapshot, error) {
	snap := Snapshot{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil // vanished mid-walk; it will show up as a change if it matters
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		snap[filepath.ToSlash(rel)] = fileState{size: info.Size(), mod: info.ModTime(), mode: info.Mode()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// ChangedSince lists the repo-relative paths that appeared or changed between the two snapshots,
// excluding the given declared paths. Deletions are reported too: a run that removed a file changed
// the tree, and a publish that says nothing about it describes a state that no longer exists.
func (before Snapshot) ChangedSince(after Snapshot, declared []string) []string {
	skip := map[string]bool{}
	for _, d := range declared {
		skip[filepath.ToSlash(d)] = true
	}

	var changed []string
	for path, now := range after {
		if skip[path] {
			continue
		}
		was, existed := before[path]
		if !existed || was != now {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if skip[path] {
			continue
		}
		if _, still := after[path]; !still {
			changed = append(changed, path+" (removed)")
		}
	}
	sort.Strings(changed)
	return changed
}
