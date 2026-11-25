package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"github.com/lambdawalker/go.infra.composer/reporting/pulumicli"
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

func TestArtifactDescriptionsAndOwnersCoverStateAndPreview(t *testing.T) {
	spec := fixture(t, "email").spec
	spec.Feature = "host-app"
	spec.ArtifactFeaturePrefixes = map[string]string{"verification.": "email-verification"}
	spec.ArtifactDescriptions = map[string]string{
		"testhost.app-records": "Stores demo verification attempts.",
		"testhost.app-lambda":  "Handles demo verification requests.",
	}
	state := `{"deployment":{"resources":[
		{"urn":"urn:pulumi:test-dev::email-verification-testhost::aws:dynamodb/table:Table::testhost.app-records","type":"aws:dynamodb/table:Table","id":"records-table"},
		{"urn":"urn:pulumi:test-dev::email-verification-testhost::aws:dynamodb/table:Table::verification.backend-transactions","type":"aws:dynamodb/table:Table","id":"transactions-table"}
	]}}`
	r := &featureReport{cli: deployCLI{spec: spec}, run: func(context.Context, string, ...string) ([]byte, error) { return []byte(state), nil }}
	r.readState(context.Background())
	if len(r.report.Artifacts) != 2 || r.report.Artifacts[0].Description != "Stores demo verification attempts." {
		t.Fatalf("missing state purpose: %+v", r.report.Artifacts)
	}
	if r.report.Artifacts[0].Feature != "host-app" || r.report.Artifacts[1].Feature != "email-verification" {
		t.Fatalf("incorrect state artifact labels: %+v", r.report.Artifacts)
	}
	preview := `{"resourcePreEvent":{"metadata":{"urn":"urn:pulumi:test-dev::email-verification-testhost::aws:lambda/function:Function::testhost.app-lambda","type":"aws:lambda/function:Function","op":"create","new":{"id":"preview-only"}}}}
{"resourcePreEvent":{"metadata":{"urn":"urn:pulumi:test-dev::email-verification-testhost::aws:kms/key:Key::verification.backend-signing","type":"aws:kms/key:Key","op":"create"}}}`
	as, err := pulumicli.ApplyPreview(r.report.Artifacts, strings.NewReader(preview))
	if err != nil {
		t.Fatal(err)
	}
	as = r.cli.ownFeature(as)
	if len(as) != 4 || as[0].Description != "Stores demo verification attempts." || as[2].Description != "Handles demo verification requests." {
		t.Fatalf("missing preview purpose: %+v", as)
	}
	if as[2].Feature != "host-app" || as[3].Feature != "email-verification" {
		t.Fatalf("incorrect preview artifact labels: %+v", as)
	}
	if as[2].ProviderID != "" {
		t.Fatal("preview-only physical ID escaped into report")
	}
}

func TestArtifactFeatureUsesLogicalNameAndLongestPrefix(t *testing.T) {
	spec := manifest{Feature: "host-app", ArtifactFeaturePrefixes: map[string]string{
		"verification.": "email-verification", "verification.cache": "cache",
	}}
	for _, tc := range []struct{ key, name, want string }{
		{"urn:pulumi:dev::host::aws:s3:Object::verification.cache-object", "index.html", "cache"},
		{"urn:pulumi:dev::host::aws:kms:Key::verification.backend-signing#previous-physical-id:old::id", "display name", "email-verification"},
		{"bootstrap:state-bucket:verification.state", "verification.state", "host-app"},
		{"urn:pulumi:dev::host::aws:lambda:Function::testhost-app-lambda", "testhost-app-lambda", "host-app"},
	} {
		if got := spec.artifactFeature(tc.key, tc.name); got != tc.want {
			t.Errorf("feature for %q = %q, want %q", tc.key, got, tc.want)
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
