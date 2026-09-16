// Package container runs a published command inside a digest-pinned image, so the environment a
// foton records is the environment the command actually ran in rather than one the config asserts.
// It shells out to a container runtime and adds no logic of its own — the same relationship the
// binaries package has to plankton/nekton.
//
// See ADR-003 for why this lives inside cockpit_publish rather than becoming a verb of its own, and
// for what it changes: inside a sandboxed agent host the cockpit is the session's entire surface, so the
// container's constraints are the boundary that keeps command execution behind one of the three
// verbs acceptable.
package container

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// workdir is where the repo is mounted inside the container. A fixed path, not the host's: the
// host path must not leak into anything the run can observe and record.
const workdir = "/work"

// maskArgs empties the parts of the repository the cockpit's own trust rests on, by mounting fresh
// tmpfs over them. The command sees empty directories; anything it writes there lands in a tmpfs
// that vanishes with the container.
//
// The mount is the whole repository, and the defaults put the trust base inside it: keys_dir,
// bin_dir, .git and cockpit.config.json are all under RepoRoot. Masking them is "empty the room"
// rather than "constrain the command", which is the only version of this that holds — a denylist of
// forbidden commands is a guessing game, an empty directory is not.
//
// The failure this closes is not adversarial, which is what makes it worth closing. `go build -o
// bin/plankton ./reference/cmd/plankton` is a legitimate command straight out of this project's own
// instructions. Run inside the container against an unmasked mount it replaces the binary — and the
// cockpit then execs bin/plankton ON THE HOST to author the record. Nothing fails; what ran and what
// was recorded simply diverge. The same shape covers .git, whose hooks the host runs at the very
// next step.
//
// The registry is masked too, which the review did not ask for. A command that can write into
// objects/ can plant records the cockpit will later read back as its own — the same silent
// divergence, one layer down. Nothing a published command legitimately does needs to reach it.
func maskArgs(cfg *config.Config) []string {
	var args []string
	for _, rel := range []string{
		cfg.Raw.Paths.KeysDir,
		cfg.Raw.Paths.BinDir,
		cfg.Raw.Paths.PlanktonDir,
		cfg.Raw.Paths.NektonDir,
		".git",
	} {
		if rel == "" {
			continue
		}
		args = append(args, "--tmpfs", path.Join(workdir, filepath.ToSlash(rel)))
	}
	// A single file cannot be tmpfs-mounted; an empty read-only bind over it is the equivalent.
	// cockpit.config.json is the ceiling every call is measured against, and config.go promises it
	// is not something a session reaches through any tool — this keeps that true once a repo executes.
	args = append(args, "--volume", "/dev/null:"+path.Join(workdir, "cockpit.config.json")+":ro")
	return args
}

// stdoutLimit caps what a run hands back. Generous for a status line, far too small to be a way of
// moving a file's contents into the answer.
const stdoutLimit = 64 << 10

// cappedBuffer keeps the first limit bytes and discards the rest, reporting that it did so. It
// never fails the write: a command is not wrong for printing a lot, it just does not get to put all
// of it in the answer.
type cappedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	// The count returned is what was HANDED IN, never what was kept. io.Copy treats a short write as
	// io.ErrShortWrite, so reporting the truncated length would abort the command's own output
	// stream — and a run would fail for having printed too much, which is not a failure.
	n := len(p)
	room := b.limit - b.Buffer.Len()
	if room <= 0 {
		b.truncated = true
		return n, nil
	}
	if len(p) > room {
		b.truncated = true
		p = p[:room]
	}
	if _, err := b.Buffer.Write(p); err != nil {
		return 0, err
	}
	return n, nil
}

func (b *cappedBuffer) String() string {
	if b.truncated {
		return b.Buffer.String() + fmt.Sprintf("\n[truncated at %d bytes]", b.limit)
	}
	return b.Buffer.String()
}

// Result is what one run produced.
type Result struct {
	Stdout string
	Stderr string
	// Image is the reference actually handed to the runtime. Publishing pins this same value, which
	// is the whole point: there is one string, so what ran and what is recorded cannot diverge.
	Image string
}

// Run executes cmd inside the configured image, with repoRoot as the working directory.
//
// A non-zero exit is a failure of the publish, not a result to record: a foton describing outputs
// that a failed run left behind would assert a reproduction that never happened.
func Run(ctx context.Context, cfg *config.Config, cmd string) (*Result, error) {
	ex := cfg.Raw.Execution
	if !ex.Enabled() {
		return nil, fmt.Errorf("container.Run called with execution disabled (no execution.image configured)")
	}

	args := []string{"run", "--rm"}
	if !ex.Network {
		args = append(args, "--network", "none")
	}
	// Mapping to the invoking user keeps outputs owned by the operator. Without it a container
	// running as root leaves files the operator cannot rewrite or clean up, and git then sees them
	// as an unstageable mess.
	args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	// Nothing the command runs needs to become more privileged than the command itself, and it
	// needs no capabilities at all to read and write files in the working directory.
	args = append(args, "--cap-drop=ALL", "--security-opt=no-new-privileges")
	args = append(args, "--volume", cfg.RepoRoot+":"+workdir, "--workdir", workdir)
	args = append(args, maskArgs(cfg)...)
	args = append(args, ex.ImageRef(), "sh", "-c", cmd)

	c := exec.CommandContext(ctx, ex.EngineOrDefault(), args...)
	var out, errBuf bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errBuf

	if err := c.Run(); err != nil {
		return nil, fmt.Errorf(
			"the command failed inside %s (%s): %w\n--- stderr ---\n%s\n--- stdout ---\n%s",
			ex.Image, ex.EngineOrDefault(), err, strings.TrimSpace(errBuf.String()), strings.TrimSpace(out.String()))
	}
	return &Result{
		Stdout: strings.TrimSpace(out.String()),
		Stderr: strings.TrimSpace(errBuf.String()),
		Image:  ex.Image,
	}, nil
}

// Version reports what the configured engine says about itself, for doctor.
func Version(ctx context.Context, cfg *config.Config) (string, error) {
	engine := cfg.Raw.Execution.EngineOrDefault()
	out, err := exec.CommandContext(ctx, engine, "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return "", fmt.Errorf("could not reach the %s engine: %w", engine, err)
	}
	return strings.TrimSpace(string(out)), nil
}
