package main

import (
	"encoding/json"
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
	var project projectRecord
	if b, err := os.ReadFile(filepath.Join(local, "project.json")); err != nil ||
		json.Unmarshal(b, &project) != nil || project.Version != 1 ||
		project.Root != root || !validProjectName(project.Name) ||
		(requestedProject != "" && requestedProject != project.Name) {
		return report, "repository"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return report, "repository"
	}
	central := filepath.Join(home, ".infra-composer", project.Name)
	var registered projectRecord
	if b, err := os.ReadFile(filepath.Join(central, "project.json")); err != nil ||
		json.Unmarshal(b, &registered) != nil || registered != project {
		return report, "repository"
	}
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
	if !pattern(`^[a-z][a-z0-9-]{0,30}$`, requestedEnv) ||
		requestedEnv == "prod" || requestedEnv == "production" {
		return report, "repository"
	}
	report.Scope.Environment = requestedEnv
	report.Scope.Stack = cli.spec.StackPrefix + requestedEnv
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
}
