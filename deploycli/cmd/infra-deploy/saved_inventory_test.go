package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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
