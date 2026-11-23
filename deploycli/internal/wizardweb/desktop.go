package wizardweb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// DesktopAvailable checks whether a browser can open in the current local session.
func DesktopAvailable(ctx context.Context) bool {
	// A forwarded display can open a browser on a different machine, where
	// the wizard's loopback address would refer to that machine instead.
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if os.Getenv(name) != "" {
			return false
		}
	}
	opener := browserCommand("")
	if _, err := exec.LookPath(opener[0]); err != nil {
		return false
	}
	return desktopSession(ctx)
}

func browserCommand(url string) []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", url}
	case "darwin":
		return []string{"/usr/bin/open", url}
	default:
		return []string{"xdg-open", url}
	}
}

func openBrowser(ctx context.Context, url string) {
	openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	args := browserCommand(url)
	if err := exec.CommandContext(openCtx, args[0], args[1:]...).Run(); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "Open the deployment wizard URL above in your browser.")
	}
}
