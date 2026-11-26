package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

type deploymentReceipt struct {
	Version                                                           int
	Project, Environment, Account, Region, Backend, Stack, PlanSHA256 string
	AppliedAt                                                         time.Time
}

type browserLaunchEnvironmentKey struct{}

func browserTargetArgs(project, environment, action string, skipGitHub bool) []string {
	var args []string
	if project != "" {
		args = append(args, "--project", project)
	}
	if environment != "" {
		args = append(args, "--environment", environment)
	}
	if action != "" {
		args = append(args, "--action", action)
	}
	if skipGitHub {
		args = append(args, "--skip-github")
	}
	return args
}

func (cli deployCLI) browserEnvironments(root, requestedProject string) ([]wizardweb.Environment, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	composerDir := filepath.Join(home, ".infra-composer")
	project := registeredProject(root, composerDir, requestedProject)
	if project == "" {
		return nil, nil
	}
	dir := filepath.Join(composerDir, project, "environments")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var environments []wizardweb.Environment
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !cli.spec.validEnvironment(name) {
			continue
		}
		status := "draft"
		private := filepath.Join(dir, name)
		var settings Settings
		data, err := os.ReadFile(filepath.Join(private, "settings.json"))
		if err == nil && json.Unmarshal(data, &settings) == nil && settings.Version == 1 &&
			settings.Environment == name && pattern(`^\d{12}$`, settings.Account) &&
			validRegion(settings.Region) && validBackend(settings.Backend) {
			status = "saved"
			var receipt deploymentReceipt
			data, err := os.ReadFile(filepath.Join(private, "applied.json"))
			if err == nil && json.Unmarshal(data, &receipt) == nil && receipt.Version == 1 &&
				receipt.Project == project && receipt.Environment == name && receipt.Account == settings.Account &&
				receipt.Region == settings.Region && receipt.Backend == settings.Backend &&
				receipt.Stack == cli.spec.stackName(project, name) && !receipt.AppliedAt.IsZero() {
				status = "deployed"
			}
		}
		environments = append(environments, wizardweb.Environment{Name: name, Status: status})
	}
	return environments, nil
}

func (cli deployCLI) resolveBrowserEnvironment(root, project, mode, name string) (wizardweb.EnvironmentTarget, error) {
	if !cli.spec.validEnvironment(name) {
		return wizardweb.EnvironmentTarget{}, errors.New("Use up to 31 lowercase letters, digits, or hyphens, starting with a letter. Production must be enabled by the project.")
	}
	if mode == "switch" {
		// The session only allows switching to an already open tab. Drafts
		// without saved settings can therefore be switched back into as well.
		return cli.browserEnvironmentTarget(root, project, name), nil
	}
	options, err := cli.browserEnvironments(root, project)
	if err != nil {
		return wizardweb.EnvironmentTarget{}, fmt.Errorf("read saved environments: %w", err)
	}
	exists := false
	for _, option := range options {
		if option.Name == name {
			exists = true
			break
		}
	}
	switch mode {
	case "new":
		if exists {
			return wizardweb.EnvironmentTarget{}, errors.New("This environment already exists. Choose Open Environment.")
		}
	case "open":
		if !exists {
			return wizardweb.EnvironmentTarget{}, errors.New("This environment is no longer saved in this project.")
		}
	default:
		return wizardweb.EnvironmentTarget{}, errors.New("Choose New Environment or Open Environment.")
	}
	return cli.browserEnvironmentTarget(root, project, name), nil
}

func (cli deployCLI) browserEnvironmentTarget(root, project, name string) wizardweb.EnvironmentTarget {
	report, source := cli.savedInventory(root, project, name)
	report.Scope.Environment = name
	return wizardweb.EnvironmentTarget{
		Inventory: report, Source: source,
		Drift:   cli.savedDrift(root, project, report.Scope),
		Fields:  cli.startupSettingsFields(root, project, name, report, source),
		History: deploymentHistory(filepath.Join(root, ".deploy"), report.Scope),
	}
}
