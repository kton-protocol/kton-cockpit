// Package container runs a published command inside a digest-pinned image, so the environment a
// foton records is the environment the command actually ran in rather than one the config asserts.
// It shells out to a container runtime and adds no logic of its own — the same relationship the
// binaries package has to plankton/nekton.
//
// See ADR-003 for why this lives inside cockpit_publish rather than becoming a verb of its own, and
// for what it changes: inside claude-science the cockpit is Claude's entire surface, so the
// container's constraints are the boundary that keeps command execution behind one of the three
// verbs acceptable.
package container

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/deathbychoco/claude-science-cockpit/internal/config"
)

// workdir is where the repo is mounted inside the container. A fixed path, not the host's: the
// host path must not leak into anything the run can observe and record.
const workdir = "/work"

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
	args = append(args,
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--volume", cfg.RepoRoot+":"+workdir,
		"--workdir", workdir,
		ex.ImageRef(),
		"sh", "-c", cmd,
	)

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
