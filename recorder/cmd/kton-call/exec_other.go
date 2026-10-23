//go:build !unix

package main

import (
	"errors"
	"os"
	"os/exec"
)

// execTool where a process cannot become another: run the tool as a child and leave with its code.
func execTool(path string, argv, env []string) error {
	cmd := exec.Command(path, argv[1:]...)
	cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = env, os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	if err == nil {
		os.Exit(0)
	}
	return err
}
