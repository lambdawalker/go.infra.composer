//go:build windows

package main

import "golang.org/x/sys/windows"

func flushTerminalInput(fd uintptr) error {
	r, _, e := windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushConsoleInputBuffer").Call(fd)
	if r == 0 {
		return e
	}
	return nil
}
