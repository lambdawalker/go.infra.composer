package wizardweb

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func desktopSession(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	name, err := exec.CommandContext(ctx, "/bin/launchctl", "managername").Output()
	return err == nil && strings.TrimSpace(string(name)) == "Aqua"
}
