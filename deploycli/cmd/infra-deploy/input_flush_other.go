//go:build !linux && !windows && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package main

import "fmt"

func flushTerminalInput(fd uintptr) error {
	return fmt.Errorf("interactive terminal input unsupported on this platform; use explicit noninteractive inputs")
}
