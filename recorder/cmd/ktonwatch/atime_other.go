//go:build !linux

package main

import (
	"os"
	"time"
)

func atime(fi os.FileInfo) time.Time { return fi.ModTime() }
