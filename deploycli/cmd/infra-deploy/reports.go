package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/lambdawalker/go.infra.composer/reporting"
	cli "github.com/lambdawalker/go.infra.composer/reporting/pulumicli"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (cli deployCLI) owner() string { return "github.com/" + cli.spec.Repository }

type reportRunner func(context.Context, string, ...string) ([]byte, error)
type featureReport struct {
	cli                       deployCLI
	bootstrap                 *reporting.Artifact
	report                    reporting.Report
	dir, stack, output, cache string
	run                       reportRunner
	previewLog                string
	applied                   bool
}

func (cli deployCLI) newFeatureReport(ctx context.Context, root, dir, stack, private, local string, s Settings, project, operation string) *featureReport {
	r := &featureReport{cli: cli, dir: dir, stack: stack, run: func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		c := toolCommand(ctx, args...)
		c.Dir = dir
		return c.Output()
	}}
	r.report = reporting.Report{Scope: reporting.Scope{Repository: cli.owner(), Environment: s.Environment, Account: s.Account, Region: s.Region, Project: cli.spec.PulumiProject, Backend: s.Backend, Stack: stack}, Metadata: reporting.Metadata{Operation: operation, Outcome: "in_progress", StartedAt: time.Now().UTC()}}
	r.output = filepath.Join(local, "reports", s.Environment, time.Now().UTC().Format("20060102T150405.000000000Z"))
	r.cache = reporting.CachePath(filepath.Join(private, "reports"), r.report.Scope)
	if b, e := r.run(ctx, root, "git", "rev-parse", "HEAD"); e == nil {
		r.report.Metadata.Revision = strings.TrimSpace(string(b))
	}
	if b, e := r.run(ctx, root, "git", "status", "--porcelain"); e == nil {
		r.report.Metadata.Dirty = len(bytes.TrimSpace(b)) > 0
	}
	if previous, e := reporting.LoadCache(r.cache, r.report.Scope); e == nil {
		r.report = reporting.Merge(r.report, previous)
	}
	r.readState(ctx)
	if b, e := os.ReadFile(filepath.Join(private, "state-bucket.json")); e == nil {
		var receipt bucketReceipt
		u, _ := url.Parse(s.Backend)
		if json.Unmarshal(b, &receipt) == nil && u != nil && receipt.Created && receipt.Name == u.Host && receipt.Account == s.Account && receipt.Region == s.Region {
			a := reporting.Artifact{Key: "bootstrap:state-bucket:" + receipt.Name, Name: receipt.Name, Type: "aws:s3/stateBucket", Provider: "aws", ProviderID: receipt.Name, Owner: cli.owner(), Feature: cli.spec.Feature, Priority: 20, Observation: reporting.Observation{Status: reporting.Unknown, Reason: reporting.CheckFailed, Source: "aws-s3-head-bucket", ObservedAt: time.Now().UTC()}}
			if _, e := r.run(ctx, root, "aws", "s3api", "head-bucket", "--bucket", receipt.Name, "--expected-bucket-owner", s.Account); e == nil {
				a.Observation.Status = reporting.Present
				a.Observation.Reason = ""
			}
			r.bootstrap = &a
		}
	}
	return r
}
func (cli deployCLI) ownFeature(as []reporting.Artifact) []reporting.Artifact {
	for i := range as {
		as[i].Owner = cli.owner()
		as[i].Feature = cli.spec.Feature
	}
	return as
}
func (r *featureReport) warn(code string) { r.report.Warnings = append(r.report.Warnings, code) }
func (r *featureReport) readState(ctx context.Context) {
	b, e := r.run(ctx, r.dir, "pulumi", "stack", "export", "--stack", r.stack)
	if e != nil {
		r.warn("state_read_failed")
		for i := range r.report.Artifacts {
			r.report.Artifacts[i].Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.CheckFailed, Source: "pulumi-state", ObservedAt: time.Now().UTC()}
		}
		return
	}
	as, e := cli.ReadState(bytes.NewReader(b), time.Now().UTC())
	clear(b)
	if e != nil {
		r.warn("state_invalid")
		for i := range r.report.Artifacts {
			r.report.Artifacts[i].Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.CheckFailed, Source: "pulumi-state", ObservedAt: time.Now().UTC()}
		}
		return
	}
	previous := r.report
	r.report.Artifacts = r.cli.ownFeature(as)
	// Retain planned actions and old identities only as unknown on replacements/removal.
	seen := map[string]bool{}
	for i := range r.report.Artifacts {
		a := &r.report.Artifacts[i]
		seen[a.Key] = true
		for _, old := range previous.Artifacts {
			if old.Key == a.Key {
				a.PlannedAction = old.PlannedAction
				if old.ProviderID != "" && old.ProviderID != a.ProviderID {
					old.Key += "#previous-physical-id:" + old.ProviderID
					old.Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.IdentityChanged}
					r.report.Artifacts = append(r.report.Artifacts, old)
				}
				break
			}
		}
	}
	for _, old := range previous.Artifacts {
		if !seen[old.Key] {
			old.Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}
			r.report.Artifacts = append(r.report.Artifacts, old)
		}
	}
}

type refreshReportRunner struct{ run reportRunner }

func (r refreshReportRunner) Run(ctx context.Context, dir string, args []string, _ io.Writer) error {
	_, e := r.run(ctx, dir, append([]string{"pulumi"}, args...)...)
	return e
}
func (r *featureReport) finish(ctx context.Context, runErr error, refresh bool) error {
	if r.previewLog != "" {
		if f, e := os.Open(r.previewLog); e == nil {
			as, e := cli.ApplyPreview(r.report.Artifacts, f)
			f.Close()
			r.report.Artifacts = r.cli.ownFeature(as)
			if e != nil {
				r.warn("preview_events_invalid")
			}
		} else {
			r.warn("preview_events_unavailable")
		}
		os.Remove(r.previewLog)
	}
	if r.applied {
		r.readState(ctx)
	}
	if refresh {
		as, e := cli.CollectRefresh(ctx, refreshReportRunner{r.run}, cli.RefreshOptions{Dir: r.dir, Stack: r.stack}, r.report.Artifacts)
		r.report.Artifacts = r.cli.ownFeature(as)
		if e != nil {
			r.warn("native_refresh_partial")
		}
	}
	if r.bootstrap != nil {
		filtered := r.report.Artifacts[:0]
		for _, a := range r.report.Artifacts {
			if !strings.HasPrefix(a.Key, "bootstrap:") {
				filtered = append(filtered, a)
			}
		}
		r.report.Artifacts = append(filtered, *r.bootstrap)
	}
	r.report.Metadata.FinishedAt = time.Now().UTC()
	r.report.Metadata.Outcome = "succeeded"
	if runErr != nil {
		r.report.Metadata.Outcome = "failed"
	} else if len(r.report.Warnings) > 0 {
		r.report.Metadata.Outcome = "incomplete"
	}
	r.report = reporting.Normalize(r.report)
	if e := writeFeatureBundle(r.output, r.report); e != nil {
		return e
	}
	if r.report.Metadata.Operation != "preview" {
		if e := reporting.SaveCache(r.cache, r.report); e != nil {
			return e
		}
	}
	wizard(ctx).heading("Feature artifact report")
	wizard(ctx).detail("HTML", filepath.Join(r.output, "artifacts.html"))
	wizard(ctx).detail("JSON / MD", r.output)
	return nil
}
func writeFeatureBundle(dir string, r reporting.Report) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	for _, f := range []struct {
		ext   string
		write func(io.Writer, reporting.Report) error
	}{{"json", reporting.WriteJSON}, {"md", reporting.WriteMarkdown}, {"html", reporting.WriteHTML}} {
		var b bytes.Buffer
		if e := f.write(&b, r); e != nil {
			return fmt.Errorf("render report: %w", e)
		}
		if e := atomicWrite(filepath.Join(dir, "artifacts."+f.ext), b.Bytes()); e != nil {
			return e
		}
	}
	return nil
}
