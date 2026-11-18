//go:build darwin || freebsd || openbsd || netbsd || dragonfly

package main

import "golang.org/x/sys/unix"

func flushTerminalInput(fd uintptr) error { return unix.IoctlSetPointerInt(int(fd), unix.TIOCFLUSH, 1) }
