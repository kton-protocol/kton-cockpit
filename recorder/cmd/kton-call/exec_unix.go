//go:build unix

package main

import "syscall"

// execTool becomes the real tool: same process, so the recorder's process tree stays as it was.
func execTool(path string, argv, env []string) error {
	return syscall.Exec(path, argv, env)
}
