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
	if args[0] == "pulumi" {
		env := make([]string, 0, len(os.Environ())+1)
		for _, v := range os.Environ() {
			if !strings.EqualFold(strings.SplitN(v, "=", 2)[0], "PULUMI_DEBUG_COMMANDS") {
				env = append(env, v)
			}
		}
		cmd.Env = append(env, "PULUMI_DEBUG_COMMANDS=true")
	}
	return cmd
}
