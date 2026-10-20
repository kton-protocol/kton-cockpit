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
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/cockpit/executor"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// hidden lists what a published command must find empty: the parts of the repository the cockpit's
// own trust rests on. The executor empties them (executor/container mounts fresh tmpfs over them).
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
//
// cockpit.config.json is the ceiling every call is measured against, and config.go promises it is
// not something a session reaches through any tool — hiding it keeps that true once a repo executes.
func hidden(cfg *config.Config) (dirs, files []string) {
	for _, rel := range []string{
		cfg.Raw.Paths.KeysDir,
		cfg.Raw.Paths.BinDir,
		cfg.Raw.Paths.PlanktonDir,
		cfg.Raw.Paths.NektonDir,
	} {
		if rel != "" {
			dirs = append(dirs, rel)
		}
	}
	// What the backend keeps for itself (git: .git) is part of the same base.
	dirs = append(dirs, cfg.Raw.Private()...)
	return dirs, []string{"cockpit.config.json"}
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
	// Removed are the paths the executor reports the command deleted (executor.Result.Removed).
	Removed []string
}

// Step is the step the configuration describes for cmd.
func Step(cfg *config.Config, cmd string) executor.Step {
	ex := cfg.Raw.Execution
	dirs, files := hidden(cfg)
	return executor.Step{
		Root:      cfg.RepoRoot,
		Image:     ex.Image,
		Engine:    ex.Engine,
		Network:   ex.Network,
		Hide:      dirs,
		HideFiles: files,
		Cmd:       cmd,
		Options:   ex.Options,
	}
}

// Run executes cmd with the configured executor, in the configured image, with repoRoot as the
// working directory.
//
// A non-zero exit is a failure of the publish, not a result to record: a foton describing outputs
// that a failed run left behind would assert a reproduction that never happened.
func Run(ctx context.Context, cfg *config.Config, cmd string) (*Result, error) {
	ex := cfg.Raw.Execution
	if !ex.Enabled() {
		return nil, fmt.Errorf("container.Run called with execution disabled (no execution.image configured)")
	}
	e, ok := executor.Lookup(ex.Executor)
	if !ok {
		return nil, fmt.Errorf("execution.executor %q is not available in this build (have: %s)",
			ex.Executor, strings.Join(executor.Names(), ", "))
	}
	if err := ensureMountPoints(cfg); err != nil {
		return nil, err
	}
	res, err := e.Run(ctx, Step(cfg, cmd))
	if err != nil {
		return nil, err
	}
	for _, p := range res.Removed {
		if c := filepath.ToSlash(filepath.Clean(p)); filepath.IsAbs(p) || c == ".." || strings.HasPrefix(c, "../") {
			return nil, fmt.Errorf("executor %q reported a removed path outside the repository: %q", ex.Executor, p)
		}
	}
	return &Result{Stdout: res.Stdout, Stderr: res.Stderr, Image: res.Image, Removed: res.Removed}, nil
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

// RunTool runs a pinned tool image (not the execution image) with dir mounted at /q, without network
// and as the invoking user: the SPARQL engine for search and profile rules. args go to the image's
// own entrypoint.
func RunTool(ctx context.Context, cfg *config.Config, image, dir string, args ...string) (string, error) {
	ref := strings.TrimPrefix(image, "oci://")
	full := []string{"run", "--rm", "--network", "none",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--volume", dir + ":/q", ref}
	full = append(full, args...)
	c := exec.CommandContext(ctx, cfg.Raw.Execution.EngineOrDefault(), full...)
	var out, errBuf bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errBuf
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w\n%s", ref, strings.Join(args, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}
