//go:build linux

package main

import "golang.org/x/sys/unix"

func flushTerminalInput(fd uintptr) error {
	return unix.IoctlSetInt(int(fd), unix.TCFLSH, unix.TCIFLUSH)
}
