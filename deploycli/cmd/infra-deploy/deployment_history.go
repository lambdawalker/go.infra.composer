package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
	"github.com/lambdawalker/go.infra.composer/reporting"
)

const (
	reportDirectoryTime = "20060102T150405.000000000Z"
	applyStatusFile     = "deployment-apply-status"
	maxHistoryReport    = 16 << 20
)

// deploymentHistory reads the latest finished deployment report of each outcome.
// The exact scope comes from verified saved settings or the active deployment.
func deploymentHistory(local string, scope reporting.Scope) []wizardweb.DeploymentHistoryRecord {
	if scope.Account == "" || scope.Region == "" || scope.Backend == "" || scope.Stack == "" ||
		!pattern(`^[a-z][a-z0-9-]{0,30}$`, scope.Environment) {
		return nil
	}
	root, err := os.OpenRoot(filepath.Dir(local))
	if err != nil {
		return nil
	}
	defer root.Close()
	path := filepath.Base(local)
	for _, part := range []string{"reports", scope.Environment} {
		info, err := root.Lstat(path)
		if err != nil || !info.IsDir() {
			return nil
		}
		path = filepath.Join(path, part)
	}
	info, err := root.Lstat(path)
	if err != nil || !info.IsDir() {
		return nil
	}
	environment, err := root.OpenRoot(path)
	if err != nil {
		return nil
	}
	defer environment.Close()
	entries, err := fs.ReadDir(environment.FS(), ".")
	if err != nil {
		return nil
	}
	var success, failure *wizardweb.DeploymentHistoryRecord
	for _, entry := range slices.Backward(entries) {
		id := entry.Name()
		if !entry.IsDir() {
			continue
		}
		if _, err := time.Parse(reportDirectoryTime, id); err != nil {
			continue
		}
		data, err := readHistoryFile(environment, filepath.Join(id, "artifacts.json"), maxHistoryReport)
		if err != nil {
			continue
		}
		var report reporting.Report
		if err := json.Unmarshal(data, &report); err != nil || report.SchemaVersion != reporting.SchemaVersion ||
			report.Scope != scope || report.Metadata.Operation != "deploy" || report.Metadata.FinishedAt.IsZero() {
			continue
		}
		switch report.Metadata.Outcome {
		case "succeeded", "incomplete":
			if success == nil {
				record := historyRecord(environment, id, report)
				success = &record
			}
		case "failed":
			if failure == nil {
				record := historyRecord(environment, id, report)
				failure = &record
			}
		}
		if success != nil && failure != nil {
			break
		}
	}
	var history []wizardweb.DeploymentHistoryRecord
	for _, record := range []*wizardweb.DeploymentHistoryRecord{success, failure} {
		if record != nil {
			history = append(history, *record)
		}
	}
	return history
}

func readHistoryFile(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, os.ErrInvalid
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit))
}

func historyRecord(root *os.Root, id string, report reporting.Report) wizardweb.DeploymentHistoryRecord {
	entry := wizardweb.DeploymentHistoryEntry{ID: id, Outcome: report.Metadata.Outcome,
		FinishedAt: report.Metadata.FinishedAt, Dirty: report.Metadata.Dirty}
	if pattern(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`, report.Metadata.Revision) {
		entry.Revision = report.Metadata.Revision
	}
	if entry.Outcome != "failed" {
		entry.ApplyStatus = "succeeded"
	}
	if status, err := readHistoryFile(root, filepath.Join(id, applyStatusFile), 32); err == nil {
		switch string(status) {
		case "not-started", "failed", "succeeded":
			entry.ApplyStatus = string(status)
		}
	}
	for _, artifact := range report.Artifacts {
		switch artifact.PlannedAction {
		case "create":
			entry.Created++
		case "update":
			entry.Updated++
		case "replace", "create-replacement", "delete-replaced":
			entry.Replaced++
		case "delete":
			entry.Deleted++
		case "", "same":
		default:
			entry.Other++
		}
	}
	return wizardweb.DeploymentHistoryRecord{DeploymentHistoryEntry: entry, Report: report}
}

func (r *featureReport) recordDeploymentHistory(ctx context.Context) {
	if r.report.Metadata.Operation != "deploy" {
		return
	}
	applyStatus := "not-started"
	if r.applyCompleted {
		applyStatus = "succeeded"
	} else if r.applied {
		applyStatus = "failed"
	}
	// The artifact bundle is authoritative. This optional marker distinguishes
	// apply failures from failures later in deployment, such as readiness checks.
	_ = atomicWrite(filepath.Join(r.output, applyStatusFile), []byte(applyStatus))
	if session := wizard(ctx).browser; session != nil {
		session.SetDeploymentHistory(deploymentHistory(r.local, r.report.Scope))
	}
}

func (r *featureReport) saveBundle(ctx context.Context) error {
	if err := writeFeatureBundle(r.output, r.report); err != nil {
		return err
	}
	r.recordDeploymentHistory(ctx)
	return nil
}

// saveEarlyDeploymentFailure records a verified target even when preparation
// fails before a normal artifact report can read the stack. It leaves the
// inventory cache intact because no evidence was collected for this attempt.
func (cli deployCLI) saveEarlyDeploymentFailure(ctx context.Context, root, dir, stack, local string, target Settings, started time.Time, runErr error) error {
	if runErr == nil || errors.Is(runErr, context.Canceled) || ctx.Err() != nil {
		return nil
	}
	attempt := cli.beginFeatureReport(ctx, root, dir, stack, local, target, "deploy", started)
	attempt.report.Metadata.Outcome = "failed"
	attempt.report.Metadata.FinishedAt = time.Now().UTC()
	attempt.warn("deployment_preparation_failed")
	return attempt.saveBundle(ctx)
}
