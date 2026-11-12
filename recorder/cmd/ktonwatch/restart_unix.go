//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restartAs replaces this process with the binary at exe, with the same arguments and environment
func restartAs(exe string) error { return syscall.Exec(exe, os.Args, os.Environ()) }
