package main

import (
	"os/exec"
	"syscall"
)

// setParentDeath: the show ends with ktonwatch
func setParentDeath(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
