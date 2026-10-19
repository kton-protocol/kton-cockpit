// Package container is the reference executor: the step runs inside a digest-pinned image through
// docker or podman (ADR-003). It shells out to the runtime and adds no logic of its own.
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

	"github.com/kton-protocol/kton-cockpit/cockpit/executor"
)

func init() { executor.Register(executor.Default, Executor{}) }

// workdir is where the repo is mounted inside the container. A fixed path, not the host's: the
// host path must not leak into anything the run can observe and record.
const workdir = "/work"

// Executor runs steps with a container runtime.
type Executor struct{}

// Engine is the runtime executable a step names, "docker" when it names none.
func Engine(s executor.Step) string {
	if s.Engine == "" {
		return "docker"
	}
	return s.Engine
}

// Run executes the step inside its image, with the repository mounted as the working directory.
func (Executor) Run(ctx context.Context, s executor.Step) (executor.Result, error) {
	args := []string{"run", "--rm"}
	if !s.Network {
		args = append(args, "--network", "none")
	}
	// Mapping to the invoking user keeps outputs owned by the operator. Without it a container
	// running as root leaves files the operator cannot rewrite or clean up, and git then sees them
	// as an unstageable mess.
	args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	// Nothing the command runs needs to become more privileged than the command itself, and it
	// needs no capabilities at all to read and write files in the working directory.
	args = append(args, "--cap-drop=ALL", "--security-opt=no-new-privileges")
	args = append(args, "--volume", s.Root+":"+workdir, "--workdir", workdir)
	args = append(args, maskArgs(s)...)
	args = append(args, strings.TrimPrefix(s.Image, "oci://"), "sh", "-c", s.Cmd)

	engine := Engine(s)
	c := exec.CommandContext(ctx, engine, args...)
	var out, errBuf bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errBuf

	if err := c.Run(); err != nil {
		return executor.Result{}, fmt.Errorf(
			"the command failed inside %s (%s): %w\n--- stderr ---\n%s\n--- stdout ---\n%s",
			s.Image, engine, err, strings.TrimSpace(errBuf.String()), strings.TrimSpace(out.String()))
	}
	return executor.Result{
		Stdout: strings.TrimSpace(out.String()),
		Stderr: strings.TrimSpace(errBuf.String()),
		Image:  s.Image,
	}, nil
}

// maskArgs empties what the step hides by mounting fresh tmpfs over each directory and an empty
// read-only bind over each file. The command sees empty places; anything it writes there lands in a
// tmpfs that vanishes with the container. Why the core hides what it hides is internal/container.
func maskArgs(s executor.Step) []string {
	var args []string
	for _, rel := range s.Hide {
		if rel == "" {
			continue
		}
		args = append(args, "--tmpfs", path.Join(workdir, filepath.ToSlash(rel)))
	}
	// A single file cannot be tmpfs-mounted; an empty read-only bind over it is the equivalent.
	for _, rel := range s.HideFiles {
		if rel == "" {
			continue
		}
		args = append(args, "--volume", "/dev/null:"+path.Join(workdir, filepath.ToSlash(rel))+":ro")
	}
	return args
}
