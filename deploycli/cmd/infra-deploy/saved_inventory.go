package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
	"github.com/lambdawalker/go.infra.composer/reporting"
)

// savedInventory is read-only. It can prepare the browser's workspace before
// the vault and AWS login, but its artifact evidence is never a live check.
// The regular deployment path validates malformed records and remote state.
func (cli deployCLI) savedInventory(root, requestedProject, requestedEnv string) (reporting.Report, string) {
	report := reporting.Report{Scope: reporting.Scope{
		Repository: cli.owner(), Project: cli.spec.PulumiProject,
	}}
	local := filepath.Join(root, ".deploy")
	home, err := os.UserHomeDir()
	if err != nil {
		return report, "repository"
	}
	composerDir := filepath.Join(home, ".infra-composer")
	project := registeredProject(root, composerDir, requestedProject)
	if project == "" {
		return report, "repository"
	}
	central := filepath.Join(composerDir, project)
	if requestedEnv == "" {
		var flags struct {
			Version     int
			Environment string
		}
		if b, err := os.ReadFile(filepath.Join(local, "flags.json")); err == nil &&
			json.Unmarshal(b, &flags) == nil && flags.Version == 1 {
			requestedEnv = flags.Environment
		}
	}
	if !cli.spec.validEnvironment(requestedEnv) {
		return report, "repository"
	}
	report.Scope.Environment = requestedEnv
	report.Scope.Stack = cli.spec.stackName(project, requestedEnv)
	private := filepath.Join(central, "environments", requestedEnv)
	var settings Settings
	if b, err := os.ReadFile(filepath.Join(private, "settings.json")); err != nil ||
		json.Unmarshal(b, &settings) != nil || settings.Version != 1 ||
		settings.Environment != requestedEnv || !pattern(`^\d{12}$`, settings.Account) ||
		!validRegion(settings.Region) || !validBackend(settings.Backend) {
		return report, "selected-target"
	}
	report.Scope.Account = settings.Account
	report.Scope.Region = settings.Region
	report.Scope.Backend = settings.Backend
	cache := reporting.CachePath(filepath.Join(private, "reports"), report.Scope)
	previous, err := reporting.LoadCache(cache, report.Scope)
	if err != nil {
		return report, "saved-target"
	}
	// A previous report describes a known inventory, not present cloud state or
	// a plan for this run. Keep only safe identities and relationships.
	for i := range previous.Artifacts {
		previous.Artifacts[i].PlannedAction = ""
		previous.Artifacts[i].Observation = reporting.Observation{Status: reporting.Unknown, Reason: reporting.Unchecked}
	}
	previous.Metadata.Revision = ""
	return previous, "saved-report"
}

func (cli deployCLI) publishSavedInventory(session *wizardweb.Session, root, project, env string) {
	if session == nil {
		return
	}
	report, source := cli.savedInventory(root, project, env)
	session.InventoryWithSource(report, source)
	session.SetSettingsFields(cli.startupSettingsFields(root, project, env, report, source))
}

// startupSettingsFields is a read-only form schema for the workspace before an
// action starts. It reads only nonsecret settings and manifest-ordered inputs;
// the live form replaces this snapshot when the user can edit it.
func (cli deployCLI) startupSettingsFields(root, requestedProject, requestedEnv string, report reporting.Report, source string) []wizardweb.Field {
	project := cli.spec.DefaultProject
	if project == "" {
		project = cli.spec.PulumiProject
	}
	if validProjectName(requestedProject) {
		project = requestedProject
	}
	environment := requestedEnv
	if report.Scope.Environment != "" {
		environment = report.Scope.Environment
	}
	if environment == "" {
		environment = "dev"
	}
	form := deploymentForm{
		project:         project,
		target:          Settings{Version: 1, Environment: environment, Region: "us-east-2", Account: "auto", Backend: "new"},
		canCreateBucket: true, hasMemory: cli.spec.Memory != 0, memory: cli.spec.Memory,
		inputs: cli.spec.inputFlags(flag.NewFlagSet("workspace-inputs", flag.ContinueOnError)),
	}
	if source != "repository" {
		// savedInventory verified that the local project belongs to this checkout
		// and that its central registry matches. Reuse that identity, not a hint.
		var record projectRecord
		if data, err := os.ReadFile(filepath.Join(root, ".deploy", "project.json")); err == nil &&
			json.Unmarshal(data, &record) == nil && record.Version == 1 && record.Root == root && validProjectName(record.Name) {
			form.project = record.Name
			if home, err := os.UserHomeDir(); err == nil && pattern(`^[a-z][a-z0-9-]{0,30}$`, environment) {
				private := filepath.Join(home, ".infra-composer", record.Name, "environments", environment)
				if defaults, err := form.inputs.defaults(cli.spec.InputsFile, private); err == nil {
					form.defaults = defaults
				}
				if report.Scope.Account != "" {
					var saved Settings
					if data, err := os.ReadFile(filepath.Join(private, "settings.json")); err == nil &&
						json.Unmarshal(data, &saved) == nil && saved.Version == 1 &&
						saved.Environment == environment && saved.Account == report.Scope.Account &&
						saved.Region == report.Scope.Region && saved.Backend == report.Scope.Backend {
						form.saved = saved
						form.target = saved
						if saved.Profile != "" {
							form.profiles = []wizardweb.Option{{Value: saved.Profile, Label: saved.Profile}}
						}
					}
				}
				if form.hasMemory {
					var flags struct{ Version, Memory int }
					if data, err := os.ReadFile(filepath.Join(private, "flags.json")); err == nil &&
						json.Unmarshal(data, &flags) == nil && flags.Version == 1 &&
						flags.Memory >= 128 && flags.Memory <= 10240 {
						form.memory = flags.Memory
					}
				}
			}
		}
	}
	fields := form.fields()
	values := make(map[string]string)
	for _, field := range fields {
		if field.Group == "Project inputs" && !field.ReadOnly {
			values[field.Name] = field.Value
		}
	}
	form.inputs, _ = resolveFeatureDraft(form.inputs, environment, values)
	return form.acceptedFields(values)
}
