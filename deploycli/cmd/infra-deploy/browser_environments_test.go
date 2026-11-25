package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

func TestBrowserEnvironmentsUseScopedDeploymentReceipts(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project := projectRecord{Version: 1, Name: "demo", Root: root}
	central := filepath.Join(home, ".infra-composer", project.Name)
	writeSavedInventoryFixture(t, filepath.Join(root, ".deploy", "project.json"), project)
	writeSavedInventoryFixture(t, filepath.Join(central, "project.json"), project)
	cli := fixture(t, "email")
	for _, env := range []string{"dev", "prod", "qa"} {
		dir := filepath.Join(central, "environments", env)
		settings := Settings{1, env, "demo-sso", "us-east-2", "123456789012", "s3://demo-state/example"}
		writeSavedInventoryFixture(t, filepath.Join(dir, "settings.json"), settings)
		receipt := deploymentReceipt{
			Version: 1, Project: project.Name, Environment: env, Account: settings.Account,
			Region: settings.Region, Backend: settings.Backend, Stack: cli.spec.stackName(project.Name, env), AppliedAt: time.Now(),
		}
		if env == "qa" {
			receipt.Account = "999999999999"
		}
		writeSavedInventoryFixture(t, filepath.Join(dir, "applied.json"), receipt)
	}
	want := []wizardweb.Environment{{Name: "dev", Status: "deployed"}, {Name: "qa", Status: "saved"}}
	got, err := cli.browserEnvironments(root, "")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("environments = %+v, err = %v", got, err)
	}
	if got, err := cli.browserEnvironments(t.TempDir(), "demo"); err != nil || len(got) != 0 {
		t.Fatalf("other checkout can see environments: %+v, %v", got, err)
	}
	if _, err := cli.resolveBrowserEnvironment(root, "", "new", "dev"); err == nil {
		t.Fatal("duplicate environment accepted")
	}
	if _, err := cli.resolveBrowserEnvironment(root, "", "open", "missing"); err == nil {
		t.Fatal("opening an unsaved target succeeded")
	}
	target, err := cli.resolveBrowserEnvironment(root, "", "open", "qa")
	if err != nil || target.Inventory.Scope.Environment != "qa" || target.Inventory.Scope.Stack != cli.spec.stackName(project.Name, "qa") {
		t.Fatalf("opened target = %+v, err = %v", target.Inventory.Scope, err)
	}
	target, err = cli.resolveBrowserEnvironment(root, "", "new", "sandbox")
	if err != nil || target.Inventory.Scope.Environment != "sandbox" || target.Inventory.Scope.Account != "" {
		t.Fatalf("new target inherited a different environment: %+v, %v", target.Inventory.Scope, err)
	}
	if _, err := os.Stat(filepath.Join(central, "environments", "sandbox")); !os.IsNotExist(err) {
		t.Fatalf("selecting a draft created deployment state: %v", err)
	}
}
