package main

import (
	"fmt"
	"os"
)

func operationLock(path string) (func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, fmt.Errorf("cannot acquire %s; check active operations before manually removing a stale lock: %w", path, e)
	}
	if _, e = fmt.Fprintf(f, "pid=%d\n", os.Getpid()); e != nil {
		f.Close()
		os.Remove(path)
		return nil, e
	}
	if e = f.Close(); e != nil {
		os.Remove(path)
		return nil, e
	}
	return func() { _ = os.Remove(path) }, nil
}
