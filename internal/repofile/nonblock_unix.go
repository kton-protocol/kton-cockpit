//go:build unix

package repofile

import "syscall"

// nonblock keeps an open of a FIFO from waiting for a writer. A regular file reads as usual with it.
const nonblock = syscall.O_NONBLOCK
