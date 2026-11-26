package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lambdawalker/go.infra.composer/reporting"
	"github.com/lambdawalker/go.infra.composer/reporting/pulumicli"
)

func driftCachePath(private string, scope reporting.Scope) string {
	return reporting.CachePath(filepath.Join(private, "reports", "drift"), scope)
}

func (cli deployCLI) savedDrift(root, requestedProject string, scope reporting.Scope) reporting.Report {
	if !cli.spec.validEnvironment(scope.Environment) || scope.Account == "" || scope.Backend == "" {
		return reporting.Report{}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return reporting.Report{}
	}
	composerDir := filepath.Join(home, ".infra-composer")
	project := registeredProject(root, composerDir, requestedProject)
	if project == "" {
		return reporting.Report{}
	}
	private := filepath.Join(composerDir, project, "environments", scope.Environment)
	report, _ := reporting.LoadCache(driftCachePath(private, scope), scope)
	return report
}

type driftCommands struct {
	backend string
	config  string
}

func (d driftCommands) output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := toolCommand(ctx, append([]string{"pulumi"}, args...)...)
	cmd.Dir = dir
	setCommandEnv(cmd, "PULUMI_BACKEND_URL", d.backend)
	// Environment-based CLI flags could silently change the refresh or its scope.
	cmd.Env = slices.DeleteFunc(cmd.Env, func(value string) bool {
		return strings.HasPrefix(strings.ToUpper(value), "PULUMI_OPTION_")
	})
	return cmd.Output()
}

func (d driftCommands) Run(ctx context.Context, dir string, args []string, _ io.Writer) error {
	_, err := d.output(ctx, dir, append(args, "--run-program=false", "--config-file", d.config)...)
	return err
}

// checkDrift runs within the main wizard's workspace/environment locks and uses
// the existing target. It writes its observation report and temporary config
// only. Pulumi reads the backend directly and previews refresh without applying.
func (cli deployCLI) checkDrift(ctx context.Context, root, private, project string, target Settings, ci bool, workers int) (runErr error) {
	if !pattern(`^\d{12}$`, target.Account) || !validRegion(target.Region) || !validBackend(target.Backend) {
		return errors.New("save an existing deployment target before checking drift")
	}
	ui := wizard(ctx)
	scope := reporting.Scope{Repository: cli.owner(), Environment: target.Environment,
		Account: target.Account, Region: target.Region, Project: cli.spec.PulumiProject,
		Backend: target.Backend, Stack: cli.spec.stackName(project, target.Environment)}
	report := reporting.Report{Scope: scope, Metadata: reporting.Metadata{Operation: "drift", StartedAt: time.Now().UTC()}}
	var currentState bool
	var inventoryOnly []reporting.Artifact
	if cached, err := reporting.LoadCache(reporting.CachePath(filepath.Join(private, "reports"), scope), scope); err == nil {
		report.Artifacts = slices.DeleteFunc(cached.Artifacts, func(artifact reporting.Artifact) bool {
			if strings.HasPrefix(artifact.Key, "urn:pulumi:") && !strings.Contains(artifact.Key, "#previous-physical-id:") {
				return false
			}
			artifact.Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}
			artifact.PlannedAction = ""
			inventoryOnly = append(inventoryOnly, artifact)
			return true
		})
		for i := range report.Artifacts {
			report.Artifacts[i].Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.CheckFailed}
		}
	}
	defer func() {
		if ctx.Err() != nil {
			return
		}
		var changed, missing, unverified, unchanged int
		for _, a := range report.Artifacts {
			switch a.Observation.Status {
			case reporting.Absent:
				missing++
			case reporting.Present:
				if a.Observation.Reason == reporting.ExternalChange {
					changed++
				} else {
					unchanged++
				}
			default:
				unverified++
			}
		}
		report.Metadata.FinishedAt = time.Now().UTC()
		report.Metadata.Outcome = "succeeded"
		if runErr != nil || unverified > 0 {
			report.Metadata.Outcome = "incomplete"
		}
		report = reporting.Normalize(report)
		if ui.browser != nil {
			if currentState {
				inventory := report
				inventory.Artifacts = append(slices.Clone(report.Artifacts), inventoryOnly...)
				ui.browser.Inventory(reporting.Normalize(inventory))
			}
			ui.browser.SetDrift(report)
		} else {
			ui.heading("Drift check")
			ui.detail("Pulumi resources", fmt.Sprintf("%d changed, %d missing, %d unverified, %d unchanged", changed, missing, unverified, unchanged))
		}
		if err := reporting.SaveCache(driftCachePath(private, scope), report); err != nil {
			runErr = errors.Join(runErr, errors.New("could not save the drift check result"))
		}
	}()
	if _, _, err := checkConnection(ctx, root, target, ci, workers, true); err != nil {
		return err
	}
	restore, err := connectionEnv(target.Profile, target.Region, ci)
	if err != nil {
		return err
	}
	defer restore()
	ui.phase("prepare", "Read current Pulumi state")
	dir := filepath.Join(root, cli.spec.PulumiDir)
	commands := driftCommands{backend: target.Backend}
	state, err := commands.output(ctx, dir, "stack", "export", "--stack", scope.Stack, "--non-interactive")
	if err != nil {
		return errors.New("could not read the saved Pulumi stack for drift verification")
	}
	defer clear(state)
	if cli.spec.StackIdentity {
		if err := verifyStackIdentity(state, project, target.Environment, false); err != nil {
			return err
		}
	}
	artifacts, err := pulumicli.ReadState(bytes.NewReader(state), report.Metadata.StartedAt)
	if err != nil {
		return errors.New("could not parse the saved Pulumi stack for drift verification")
	}
	report.Artifacts = cli.ownFeature(artifacts)
	currentState = true
	for i := range report.Artifacts {
		report.Artifacts[i].Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.CheckFailed}
	}
	tmp, err := os.MkdirTemp("", "composer-drift-")
	if err != nil {
		return errors.New("could not create a temporary config for the drift check")
	}
	defer os.RemoveAll(tmp)
	commands.config = filepath.Join(tmp, "Pulumi."+scope.Stack+".yaml")
	if _, err := commands.output(ctx, dir, "config", "refresh", "--force", "--stack", scope.Stack,
		"--config-file", commands.config, "--non-interactive"); err != nil {
		return errors.New("could not read the stack configuration for drift verification")
	}
	ui.phase("preview", "Check for drift")
	report.Artifacts, err = pulumicli.CollectRefresh(ctx, commands, pulumicli.RefreshOptions{Dir: dir, Stack: scope.Stack}, report.Artifacts)
	return err
}
