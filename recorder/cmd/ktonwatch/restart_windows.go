//go:build windows

package main

import "errors"

func restartAs(string) error { return errors.New("restart it by hand on Windows") }
