package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewBrowserVault(t *testing.T) {
	for _, location := range []string{"fresh", ".deploy/environments/dev/vault.bin", ".deploy/environments/qa/credentials.bin", ".deploy/dev/vault.json", "legacy/dev/vault.bin"} {
		t.Run(location, func(t *testing.T) {
			root := t.TempDir()
			legacy := filepath.Join(root, "legacy")
			if location != "fresh" {
				path := filepath.Join(root, location)
				os.MkdirAll(filepath.Dir(path), 0700)
				os.WriteFile(path, []byte("existing encrypted bytes"), 0600)
			}
			create, err := newBrowserVault(root, legacy)
			if err != nil || create != (location == "fresh") {
				t.Fatalf("create=%v err=%v", create, err)
			}
		})
	}
}
