package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPulumiEventLogEnvironment(t *testing.T) {
	t.Setenv("PULUMI_DEBUG_COMMANDS", "false")
	cmd := toolCommand(context.Background(), "pulumi", "preview", "--event-log", "private.json")
	count := 0
	for _, v := range cmd.Env {
		if strings.HasPrefix(v, "PULUMI_DEBUG_COMMANDS=") {
			count++
			if v != "PULUMI_DEBUG_COMMANDS=true" {
				t.Fatal(v)
			}
		}
	}
	if count != 1 || os.Getenv("PULUMI_DEBUG_COMMANDS") != "false" {
		t.Fatal("child-only override missing")
	}
	if toolCommand(context.Background(), "aws", "sts").Env != nil {
		t.Fatal("non-Pulumi environment changed")
	}
}
func TestPulumiPreviewAndRefreshAcceptEventLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mock executable")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
[ "$PULUMI_DEBUG_COMMANDS" = true ] || exit 42
exit 0
`
	if e := os.WriteFile(filepath.Join(dir, "pulumi"), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, op := range []string{"preview", "refresh"} {
		if e := toolCommand(context.Background(), "pulumi", op, "--event-log", "private.json").Run(); e != nil {
			t.Fatal(op, e)
		}
	}
}
