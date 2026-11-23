//go:build !windows && !darwin

package wizardweb

import (
	"context"
	"os"
)

func desktopSession(context.Context) bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}
