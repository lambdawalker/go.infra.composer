package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
)

func privateDirectory(path string) error {
	if e := os.MkdirAll(path, 0700); e != nil {
		return e
	}
	if runtime.GOOS != "windows" {
		return os.Chmod(path, 0700)
	}
	owner, e := user.Current()
	if e != nil {
		return e
	}
	// Windows ignores POSIX mode bits. Disable inherited access on private roots;
	// descendants inherit access for the current operator and SYSTEM only.
	c := exec.Command("icacls", path, "/inheritance:r", "/grant:r", owner.Username+":(OI)(CI)F", "*S-1-5-18:(OI)(CI)F")
	if e = c.Run(); e != nil {
		return fmt.Errorf("restrict private directory ACL %s: %w", path, e)
	}
	return nil
}
