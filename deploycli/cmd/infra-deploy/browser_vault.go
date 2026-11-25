package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Inspect filenames only, before asking for credentials. A saved vault anywhere
// in this checkout or its supported legacy location means unlock, not reset.
// Errors are not evidence that a vault is absent.
func newBrowserVault(root, legacy string) (bool, error) {
	found := false
	for _, dir := range []string{filepath.Join(root, ".deploy"), legacy} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return false, err
		}
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("cannot determine vault setup through symlink %s", path)
			}
			if !entry.IsDir() && (entry.Name() == "vault.bin" || entry.Name() == "vault.json" || entry.Name() == "credentials.bin") {
				found = true
			}
			return nil
		})
		if err != nil {
			return false, fmt.Errorf("inspect existing vault records: %w", err)
		}
	}
	return !found, nil
}
