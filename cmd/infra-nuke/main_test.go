package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lambdawalker/go.infra.composer/teardown"
	"github.com/lambdawalker/go.infra.composer/teardown/pulumicli"
)

type cliDriver struct {
	destroyed int
	present   bool
}

func (d *cliDriver) Discover(context.Context, teardown.Scope) ([]teardown.Target, error) {
	return []teardown.Target{{ID: "dev", Environment: "dev", Region: "us-east-2"}}, nil
}
func (d *cliDriver) Inventory(context.Context, teardown.Target) ([]teardown.Artifact, error) {
	return []teardown.Artifact{{ID: "data", Kind: "table", Region: "us-east-2", TargetID: "dev"}}, nil
}
func (d *cliDriver) Destroy(context.Context, teardown.Target) error {
	d.destroyed++
	d.present = false
	return nil
}
func (d *cliDriver) Inspect(context.Context, teardown.Artifact) (teardown.State, error) {
	if d.present {
		return teardown.StatePresent, nil
	}
	return teardown.StateAbsent, nil
}
func (d *cliDriver) RemoveRecovery(context.Context, teardown.Artifact) error {
	panic("unexpected recovery")
}

func TestNukeCLIConfirmationAndReport(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "nuke.json")
	report := filepath.Join(dir, "report.json")
	if err := os.WriteFile(cfg, []byte(`{"scope":{"application":"demo","account":"123456789012"},"projects":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	d := &cliDriver{present: true}
	factory := func(pulumicli.Config) (teardown.Driver, error) { return d, nil }
	var out, stderr bytes.Buffer
	args := []string{"-config", cfg, "-report", report, "-nuke"}
	if code := run(args, strings.NewReader("wrong\n"), &out, &stderr, factory); code == 0 || d.destroyed != 0 {
		t.Fatalf("confirmation bypass: %d %d", code, d.destroyed)
	}
	out.Reset()
	stderr.Reset()
	if code := run(args, strings.NewReader("nuke demo 123456789012\n"), &out, &stderr, factory); code != 0 {
		t.Fatal(code, stderr.String())
	}
	if d.destroyed != 1 || !strings.Contains(out.String(), "REGION") || !strings.Contains(out.String(), "table") {
		t.Fatal(out.String(), d.destroyed)
	}
	data, err := os.ReadFile(report)
	var r teardown.Report
	if err != nil || json.Unmarshal(data, &r) != nil || r.Scope.Application != "demo" {
		t.Fatal(string(data), err)
	}
	for _, mode := range []string{"-check", "-fast-check"} {
		out.Reset()
		stderr.Reset()
		if code := run([]string{"-config", cfg, "-report", report, mode}, strings.NewReader(""), &out, &stderr, factory); code != 0 {
			t.Fatal(mode, code, stderr.String())
		}
		if d.destroyed != 1 {
			t.Fatal("check destroyed resources")
		}
	}
}

func TestNukeCLIInvalidModesAndConfig(t *testing.T) {
	for _, args := range [][]string{{}, {"-nuke", "-check"}, {"-nuke", "extra"}} {
		var out, stderr bytes.Buffer
		if run(args, strings.NewReader(""), &out, &stderr, nil) != 2 {
			t.Fatal(args)
		}
	}
}

func TestNukeCLIRejectsLockedReport(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "nuke.json")
	report := filepath.Join(dir, "report.json")
	os.WriteFile(cfg, []byte(`{"scope":{"application":"demo","account":"123456789012"},"projects":[]}`), 0600)
	os.WriteFile(report+".lock", []byte("pid old"), 0600)
	var out, stderr bytes.Buffer
	d := &cliDriver{}
	code := run([]string{"-config", cfg, "-report", report, "-check"}, strings.NewReader(""), &out, &stderr, func(pulumicli.Config) (teardown.Driver, error) { return d, nil })
	if code == 0 || d.destroyed != 0 || !strings.Contains(stderr.String(), "locked") {
		t.Fatal(code, stderr.String())
	}
}
