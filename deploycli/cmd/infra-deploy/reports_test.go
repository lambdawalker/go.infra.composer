package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFeatureStateSafeAndOwned(t *testing.T) {
	raw := `{"deployment":{"resources":[{"urn":"urn:pulumi:dev::bench::aws:lambda/function:Function::ocr","type":"aws:lambda/function:Function","id":"fn-123","outputs":{"memorySize":1024,"timeout":90,"environment":{"SECRET":"never-export-this"},"policy":"private-policy"}}]}}`
	r := &featureReport{cli: fixture(t, "ocr"), run: func(context.Context, string, ...string) ([]byte, error) { return []byte(raw), nil }}
	r.readState(context.Background())
	if len(r.report.Artifacts) != 1 {
		t.Fatal(r.report)
	}
	a := r.report.Artifacts[0]
	if a.Owner != fixture(t, "ocr").owner() || a.Feature != "card-capture" || a.Observation.Status != reporting.Recorded {
		t.Fatal(a)
	}
	dir := t.TempDir()
	if e := writeFeatureBundle(dir, r.report); e != nil {
		t.Fatal(e)
	}
	for _, ext := range []string{"json", "md", "html"} {
		b, e := os.ReadFile(filepath.Join(dir, "artifacts."+ext))
		if e != nil || bytes.Contains(b, []byte("never-export-this")) || bytes.Contains(b, []byte("private-policy")) {
			t.Fatal(ext, e, string(b))
		}
	}
}
func TestFeatureStateReadFailureInvalidatesPresence(t *testing.T) {
	r := &featureReport{cli: fixture(t, "ocr"), report: reporting.Report{Artifacts: []reporting.Artifact{{Key: "x", ProviderID: "old", Observation: reporting.Observation{Status: reporting.Present, ObservedAt: time.Now()}}}}, run: func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("sensitive provider diagnostic")
	}}
	r.readState(context.Background())
	if r.report.Artifacts[0].Observation.Status != reporting.Unknown || strings.Contains(strings.Join(r.report.Warnings, " "), "sensitive") {
		t.Fatal(r.report)
	}
}
