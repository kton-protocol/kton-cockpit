//go:build !unix

package repofile

// nonblock is 0 where the platform has no such flag; the Lstat and SameFile checks still refuse a
// file that is not regular.
const nonblock = 0
