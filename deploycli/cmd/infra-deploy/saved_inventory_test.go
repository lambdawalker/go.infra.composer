package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
	"github.com/lambdawalker/go.infra.composer/reporting"
)

func writeSavedInventoryFixture(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStartupSettingsShowSavedTargetAndManifestInputs(t *testing.T) {
	spec := composedFixture(t)
	if err := spec.validate(); err != nil {
		t.Fatal(err)
	}
	cli := deployCLI{spec}
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := projectRecord{Version: 1, Name: "demo", Root: root}
	local := filepath.Join(root, ".deploy")
	private := filepath.Join(home, ".infra-composer", project.Name)
	writeSavedInventoryFixture(t, filepath.Join(local, "project.json"), project)
	writeSavedInventoryFixture(t, filepath.Join(local, "flags.json"), struct {
		Version     int
		Environment string
	}{1, "dev"})
	writeSavedInventoryFixture(t, filepath.Join(private, "project.json"), project)
	envDir := filepath.Join(private, "environments", "dev")
	saved := Settings{Version: 1, Environment: "dev", Profile: "attest", Account: "123456789012", Region: "us-west-2", Backend: "s3://test-state/project"}
	writeSavedInventoryFixture(t, filepath.Join(envDir, "settings.json"), saved)
	writeSavedInventoryFixture(t, filepath.Join(envDir, spec.InputsFile), map[string]any{
		"Version": 1, "ComposeInputs": map[string]string{"Sender": "custom", "Host": "mobile"},
		"Recipient": "test@example.test", "Domain": "example.test",
	})

	report, source := cli.savedInventory(root, "", "")
	fields := cli.startupSettingsFields(root, "", "", report, source)
	get := func(name string) wizardweb.Field {
		t.Helper()
		for _, field := range fields {
			if field.Name == name {
				return field
			}
		}
		t.Fatalf("missing %s", name)
		return wizardweb.Field{}
	}
	if source != "saved-target" || get("project").Value != "demo" || report.Scope.Environment != "dev" ||
		get("profile").Value != "attest" || get("region").Value != "us-west-2" ||
		get("account").Value != saved.Account || get("backend").Value != saved.Backend {
		t.Fatalf("saved target not reflected: %s %+v", source, fields[:6])
	}
	for _, field := range fields {
		if field.Name == "environment" {
			t.Fatal("environment selector still appears in Settings")
		}
	}
	if get("feature.Sender").Value != "custom" || get("feature.Host").Value != "mobile" ||
		get("feature.Recipient").Value != "test@example.test" || get("feature.Domain").Value != "example.test" {
		t.Fatalf("saved feature inputs absent: %+v", fields)
	}
	var names []string
	for _, field := range fields {
		if field.Group == "Project inputs" {
			names = append(names, field.Name)
		}
	}
	if !slices.Equal(names, []string{"feature.Recipient", "feature.Domain", "feature.Sender", "feature.Host"}) {
		t.Fatalf("manifest dependency order changed: %v", names)
	}
	for _, name := range []string{cloudflareField, githubField} {
		field := get(name)
		if field.Kind != "password" || field.Value != "" || field.Saved {
			t.Fatalf("credential exposed before vault unlock: %+v", field)
		}
	}
	derived := composedFixture(t)
	host := derived.Inputs[len(derived.Inputs)-1]
	host.GitHubVariable = ""
	host.Compose.DefaultInput = ""
	prefix := "demo"
	host.Compose.Parameters["Prefix"] = parameterBinding{Source: "literal", Value: &prefix}
	derived.Inputs[len(derived.Inputs)-1] = host
	if err := derived.validate(); err != nil {
		t.Fatal(err)
	}
	var resolvedHost string
	for _, field := range (deployCLI{derived}).startupSettingsFields(root, "", "", report, source) {
		if field.Name == "feature.Host" {
			resolvedHost = field.Value
		}
	}
	if resolvedHost != "demo.example.test" {
		t.Fatalf("saved inputs did not resolve the dependent field: %q", resolvedHost)
	}

	fallback := cli.startupSettingsFields(root, "other", "qa", reporting.Report{}, "repository")
	if fallback[0].Value != "other" {
		t.Fatalf("new project not shown: %+v", fallback[0])
	}
	for _, field := range fallback {
		if field.Name == "feature.Sender" && field.Value != "verify" {
			t.Fatalf("composed default absent: %+v", field)
		}
	}
}

func TestSavedInventoryShowsRecordedNamesWithoutClaimingLivePresence(t *testing.T) {
	cli := fixture(t, "email")
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := projectRecord{Version: 1, Name: "demo", Root: root}
	local := filepath.Join(root, ".deploy")
	private := filepath.Join(home, ".infra-composer", project.Name)
	writeSavedInventoryFixture(t, filepath.Join(local, "project.json"), project)
	writeSavedInventoryFixture(t, filepath.Join(local, "flags.json"), struct {
		Version     int
		Environment string
	}{1, "dev"})
	writeSavedInventoryFixture(t, filepath.Join(private, "project.json"), project)
	settings := Settings{Version: 1, Environment: "dev", Account: "123456789012", Region: "us-east-2", Backend: "s3://demo-state/infra-composer/demo"}
	envDir := filepath.Join(private, "environments", "dev")
	writeSavedInventoryFixture(t, filepath.Join(envDir, "settings.json"), settings)
	scope := reporting.Scope{
		Repository: cli.owner(), Project: cli.spec.PulumiProject, Environment: "dev",
		Stack: cli.spec.StackPrefix + "dev", Account: settings.Account, Region: settings.Region, Backend: settings.Backend,
	}
	old := reporting.Report{
		Scope: scope, Metadata: reporting.Metadata{Revision: "prior-commit"},
		Artifacts: []reporting.Artifact{{
			Key: "urn:example:lambda", Name: "demo-lambda", Type: "aws:lambda/function:Function",
			PlannedAction: "delete", ProviderID: "sensitive-internal-id",
			Observation: reporting.Observation{Status: reporting.Present, ObservedAt: time.Now().UTC()},
		}},
	}
	cache := reporting.CachePath(filepath.Join(envDir, "reports"), scope)
	if err := reporting.SaveCache(cache, reporting.Normalize(old)); err != nil {
		t.Fatal(err)
	}

	got, source := cli.savedInventory(root, "", "")
	if source != "saved-report" || got.Scope != scope || got.Metadata.Revision != "" || len(got.Artifacts) != 1 {
		t.Fatalf("unexpected bootstrap inventory: source=%s report=%+v", source, got)
	}
	a := got.Artifacts[0]
	if a.Name != "demo-lambda" || a.PlannedAction != "" || a.Observation.Status != reporting.Unknown || a.Observation.Reason != reporting.Unchecked {
		t.Fatalf("stale cached evidence leaked into bootstrap view: %+v", a)
	}
	if other, source := cli.savedInventory(root, "another-project", "dev"); source != "repository" || len(other.Artifacts) != 0 || other.Scope.Environment != "" {
		t.Fatalf("a different project reused local records: %s %+v", source, other)
	}
	if other, source := cli.savedInventory(root, "demo", "qa"); source != "selected-target" || len(other.Artifacts) != 0 || other.Scope.Environment != "qa" || other.Scope.Stack != cli.spec.StackPrefix+"qa" {
		t.Fatalf("a different environment reused dev records: %s %+v", source, other)
	}
	old.Scope.Account = "999999999999"
	writeSavedInventoryFixture(t, cache, reporting.Normalize(old))
	if other, source := cli.savedInventory(root, "demo", "dev"); source != "saved-target" || len(other.Artifacts) != 0 || other.Scope.Account != settings.Account {
		t.Fatalf("a mismatched report was trusted: %s %+v", source, other)
	}
}
