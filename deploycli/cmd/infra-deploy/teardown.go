package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

type teardownBatchKey struct{}

type teardownBatch struct {
	project   string
	workspace string
	targets   map[string]Settings
}

// destroyAll works through the normal single-environment teardown path so each
// entry is retired before its own stack is destroyed. The shared index is last.
func (cli deployCLI) destroyAll(ctx context.Context, project, indexBackend string, ci bool) error {
	if ci {
		return errors.New("all-environments teardown requires interactive confirmation")
	}
	if cli.spec.DiscoveryStack == nil {
		return errors.New("this repository has no declared shared discovery stack")
	}
	ui := wizard(ctx)
	if !validBackend(indexBackend) {
		var err error
		indexBackend, err = ui.input("Shared discovery index S3 state URL", "", "")
		if err != nil {
			return err
		}
		if !validBackend(indexBackend) {
			return errors.New("enter the existing s3://bucket/prefix for the shared discovery index")
		}
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	name, local, central, err := cli.projectPaths(ctx, root, project, false)
	if err != nil {
		return err
	}
	batch := &teardownBatch{project: name, workspace: local, targets: make(map[string]Settings)}
	if ui.browser != nil {
		unlock, err := operationLock(filepath.Join(local, "workspace.lock"))
		if err != nil {
			return err
		}
		defer unlock()
	}
	dirs, err := os.ReadDir(filepath.Join(central, "environments"))
	if err != nil {
		return fmt.Errorf("list saved environments: %w", err)
	}
	var environments []string
	var indexAccess Settings
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		if !pattern(`^[a-z][a-z0-9-]{0,30}$`, dir.Name()) {
			return fmt.Errorf("invalid saved environment name %q", dir.Name())
		}
		var saved Settings
		b, err := os.ReadFile(filepath.Join(central, "environments", dir.Name(), "settings.json"))
		if err != nil || json.Unmarshal(b, &saved) != nil || saved.Version != 1 || saved.Environment != dir.Name() ||
			!validBackend(saved.Backend) || (ui.browser != nil &&
			(!pattern(`^\d{12}$`, saved.Account) || !validRegion(saved.Region) || saved.Profile == "")) {
			return fmt.Errorf("saved target for %q is incomplete; resolve it before teardown", dir.Name())
		}
		environments = append(environments, dir.Name())
		batch.targets[dir.Name()] = saved
		if indexAccess.Account == "" {
			indexAccess = saved
		} else if indexAccess.Account != saved.Account || indexAccess.Region != saved.Region {
			return errors.New("saved environments span multiple AWS accounts or regions; remove them individually")
		}
	}
	slices.Sort(environments)
	if len(environments) == 0 {
		return errors.New("no saved deployment environments found")
	}
	indexPassphrase, err := ui.secret("Shared discovery index Pulumi passphrase")
	if err != nil {
		return err
	}
	if indexPassphrase == "" {
		return errors.New("shared index Pulumi passphrase required")
	}
	resetPassphrase, err := setRunEnv("PULUMI_CONFIG_PASSPHRASE", indexPassphrase)
	if err != nil {
		return err
	}
	defer resetPassphrase()
	_, _, err = checkConnection(ctx, root, indexAccess, false, 2, false)
	if err != nil {
		return fmt.Errorf("index account access: %w", err)
	}
	restoreAWS, err := connectionEnv(indexAccess.Profile, indexAccess.Region, false)
	if err != nil {
		return err
	}
	defer restoreAWS()
	index := cli.spec.DiscoveryStack
	indexDir := filepath.Join(root, index.PulumiDir)
	if err := command(ctx, indexDir, "pulumi", "login", indexBackend, "--non-interactive"); err != nil {
		return err
	}
	raw, err := outputIn(ctx, indexDir, "pulumi", "stack", "ls", "--json", "--non-interactive")
	if err != nil {
		return err
	}
	if err := requireDiscoveryStack(raw, *index); err != nil {
		return err
	}
	ui.note("Environments: " + strings.Join(environments, ", "))
	ui.note("Shared discovery index: " + index.PulumiProject + "/" + index.Stack)
	ui.note("Index state: " + indexBackend)
	if ui.browser != nil {
		targets := make([]string, 0, len(environments)+1)
		for _, environment := range environments {
			targets = append(targets, environment+" · "+cli.spec.stackName(name, environment))
		}
		targets = append(targets, "Shared index · "+index.PulumiProject+"/"+index.Stack)
		if err := ui.browser.ConfirmTeardown(ctx, wizardweb.TeardownTarget{
			Title: "Remove all environments and index", Name: name,
			Targets: targets,
			Effects: []string{
				fmt.Sprintf("Pulumi will permanently delete resources managed by the listed stacks in AWS account %s, region %s.", indexAccess.Account, indexAccess.Region),
				"Composer will retire each environment from discovery and remove the shared index after every environment succeeds.",
				"If an environment cannot be removed, teardown stops before removing the index.",
			},
			Intent: "I want to remove all environments and the index", Final: "Remove all environments and index", Type: name,
		}); err != nil {
			return err
		}
		ctx = context.WithValue(ctx, teardownBatchKey{}, batch)
	} else if err := ui.deliberate("Type the project name to approve all environments and index teardown", name); err != nil {
		return err
	}
	for _, environment := range environments {
		ui.note("Removing environment " + environment)
		// The destroy action verifies each target again before retiring discovery.
		if err := cli.runWizard(ctx, []string{"--action", "destroy", "--project", name, "--environment", environment, "--terminal"}, false); err != nil {
			return fmt.Errorf("environment %s: %w", environment, err)
		}
	}
	stack := index.Stack
	if ui.browser == nil {
		if err := ui.deliberate("Type the shared discovery stack name to destroy its index", stack); err != nil {
			return err
		}
	}
	// A nested destroy may have switched Pulumi to another backend.
	if err := command(ctx, indexDir, "pulumi", "login", indexBackend, "--non-interactive"); err != nil {
		return err
	}
	raw, err = outputIn(ctx, indexDir, "pulumi", "stack", "ls", "--json", "--non-interactive")
	if err != nil {
		return err
	}
	if err := requireDiscoveryStack(raw, *index); err != nil {
		return err
	}
	ui.phase("apply", "Remove shared discovery index")
	return command(ctx, indexDir, "pulumi", "destroy", "--stack", stack, "--yes")
}

func requireDiscoveryStack(raw []byte, index discoveryStackSpec) error {
	var stacks []struct{ Name string }
	if json.Unmarshal(raw, &stacks) != nil {
		return errors.New("invalid shared discovery stack listing")
	}
	for _, stack := range stacks {
		if stack.Name == index.Stack ||
			strings.HasSuffix(stack.Name, "/"+index.PulumiProject+"/"+index.Stack) {
			return nil
		}
	}
	return errors.New("shared discovery stack is missing from the selected S3 backend")
}
