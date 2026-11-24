package main

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// Pulumi's internal --event-log flag is exposed only with its debug-command switch.
// Apply this to each Pulumi child, including read-only report verification.
func toolCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	credentials := credentialsFrom(ctx)
	switch args[0] {
	case "pulumi":
		setCommandEnv(cmd, "PULUMI_DEBUG_COMMANDS", "true")
		if credentials.Cloudflare != "" {
			setCommandEnv(cmd, "CLOUDFLARE_API_TOKEN", credentials.Cloudflare)
		}
	case "gh":
		if credentials.GitHub != "" {
			setCommandEnv(cmd, "GH_TOKEN", credentials.GitHub)
		}
	}
	return cmd
}

func setCommandEnv(cmd *exec.Cmd, key, value string) {
	current := cmd.Env
	if current == nil {
		current = os.Environ()
	}
	env := make([]string, 0, len(current)+1)
	for _, item := range current {
		name, _, _ := strings.Cut(item, "=")
		if !strings.EqualFold(name, key) {
			env = append(env, item)
		}
	}
	cmd.Env = append(env, key+"="+value)
}
