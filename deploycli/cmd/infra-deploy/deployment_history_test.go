package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

func TestDeploymentHistorySelectsOutcomesInExactScope(t *testing.T) {
	local := filepath.Join(t.TempDir(), ".deploy")
	scope := reporting.Scope{Environment: "dev", Account: "123456789012", Region: "us-west-2",
		Backend: "s3://history/scope", Project: "project", Repository: "github.com/test/repo", Stack: "test-dev"}
	commit := strings.Repeat("a", 40)
	addReport := func(minute int, reportScope reporting.Scope, outcome, operation, apply string) string {
		t.Helper()
		at := time.Date(2026, time.November, 1, 19, minute, 0, 0, time.UTC)
		id := at.Format(reportDirectoryTime)
		dir := filepath.Join(local, "reports", reportScope.Environment, id)
		r := reporting.Report{
			Scope: reportScope, Metadata: reporting.Metadata{Outcome: outcome, Operation: operation, Revision: commit, FinishedAt: at},
			Artifacts: []reporting.Artifact{
				{Key: "lambda", PlannedAction: "update"},
				{Key: "table", PlannedAction: "create-replacement"},
				{Key: "bucket", PlannedAction: "same"},
			},
		}
		if err := writeFeatureBundle(dir, r); err != nil {
			t.Fatal(err)
		}
		if apply != "" {
			if err := os.WriteFile(filepath.Join(dir, applyStatusFile), []byte(apply), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	addReport(0, scope, "succeeded", "deploy", "")
	wantSuccess := addReport(1, scope, "incomplete", "deploy", "")
	addReport(2, scope, "failed", "deploy", "failed")
	wantFailure := addReport(3, scope, "failed", "deploy", "succeeded")
	addReport(4, scope, "succeeded", "preview", "")
	addReport(5, scope, "cancelled", "deploy", "not-started")
	otherScope := scope
	otherScope.Backend = "s3://other/scope"
	addReport(6, otherScope, "succeeded", "deploy", "")
	otherScope = scope
	otherScope.Environment = "qa"
	addReport(7, otherScope, "succeeded", "deploy", "")

	got := deploymentHistory(local, scope)
	if len(got) != 2 || got[0].ID != wantSuccess || got[1].ID != wantFailure {
		t.Fatalf("wrong reports selected: %+v", got)
	}
	for _, record := range got {
		if record.ApplyStatus != "succeeded" || record.Revision != commit ||
			record.Updated != 1 || record.Replaced != 1 || record.Other != 0 {
			t.Errorf("incorrect history summary: %+v", record.DeploymentHistoryEntry)
		}
	}
}

func TestHistoryRecordsPreparationFailureWithoutCloudEvidence(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, ".deploy")
	active := fixture(t, "email")
	target := Settings{
		Environment: "dev", Account: "123456789012", Region: "us-west-2", Backend: "s3://history/scope",
	}
	const stack = "test-dev"
	started := time.Now().UTC().Add(-time.Second)
	err := errors.New("provider message with sensitive raw output")
	if err := active.saveEarlyDeploymentFailure(t.Context(), root, root, stack, local, target, started, err); err != nil {
		t.Fatal(err)
	}
	scope := reporting.Scope{
		Environment: target.Environment, Account: target.Account, Region: target.Region, Backend: target.Backend,
		Project: active.spec.PulumiProject, Repository: active.owner(), Stack: stack,
	}
	got := deploymentHistory(local, scope)
	if len(got) != 1 || got[0].Outcome != "failed" || got[0].ApplyStatus != "not-started" || len(got[0].Report.Artifacts) != 0 {
		t.Fatalf("wrong history for preparation failure: %+v", got)
	}
	path := filepath.Join(local, "reports", "dev", got[0].ID, "artifacts.json")
	data, readErr := os.ReadFile(path)
	if readErr != nil || strings.Contains(string(data), err.Error()) {
		t.Fatalf("preparation failure report error = %v", readErr)
	}
}
