//go:build !windows && !darwin

package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAWSWebSSOLoginArgs(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "xdg-open"), []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("DISPLAY", ":0")
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")

	base := []string{"aws", "sso", "login", "--profile", "sso-test"}
	ctx := context.Background()
	if args := awsSSOLoginArgs(ctx, "sso-test", false); !slices.Equal(args, base) {
		t.Fatalf("terminal login args = %v", args)
	}
	web := append(slices.Clone(base), "--use-device-code")
	if args := awsSSOLoginArgs(ctx, "sso-test", true); !slices.Equal(args, web) {
		t.Fatalf("desktop web login args = %v", args)
	}
	t.Setenv("SSH_CONNECTION", "remote")
	remote := append(slices.Clone(web), "--no-browser")
	if args := awsSSOLoginArgs(ctx, "sso-test", true); !slices.Equal(args, remote) {
		t.Fatalf("remote web login args = %v", args)
	}
}
