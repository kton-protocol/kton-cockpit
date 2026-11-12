//go:build !linux

package main

import "os/exec"

func setParentDeath(cmd *exec.Cmd) {}
