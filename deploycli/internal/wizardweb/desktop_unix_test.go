//go:build !windows && !darwin

package wizardweb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopAvailable(t *testing.T) {
	for _, tc := range []struct {
		name, display, wayland, ssh string
		opener, want                bool
	}{
		{name: "headless", opener: true},
		{name: "X11", display: ":1", opener: true, want: true},
		{name: "Wayland", wayland: "wayland-0", opener: true, want: true},
		{name: "SSH with forwarded X11", display: "localhost:10.0", ssh: "remote", opener: true},
		{name: "no opener", display: ":0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			t.Setenv("DISPLAY", tc.display)
			t.Setenv("WAYLAND_DISPLAY", tc.wayland)
			t.Setenv("SSH_CONNECTION", tc.ssh)
			t.Setenv("SSH_CLIENT", "")
			t.Setenv("SSH_TTY", "")
			if tc.opener {
				if err := os.WriteFile(filepath.Join(bin, "xdg-open"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if got := DesktopAvailable(t.Context()); got != tc.want {
				t.Fatalf("DesktopAvailable() = %v, want %v", got, tc.want)
			}
		})
	}
}
