package main

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// These are snapshots of actual consumer manifests, not simplified fixtures.
func TestExistingConsumerDefaults(t *testing.T) {
	for _, name := range []string{"email", "ocr"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/consumers/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var m manifest
			if err = json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			if err = m.validate(); err != nil {
				t.Fatal(err)
			}
			cli := deployCLI{m}
			if m.BuildWorkingDir != "" || m.PrepareBuild != nil || m.HealthCheck != nil || m.AllowProduction || m.BindDeploymentInputs || m.RefreshBeforePreview {
				t.Fatal("new application behavior enabled by default")
			}
			if m.stackName("any-project", "dev") != m.StackPrefix+"dev" {
				t.Fatal("legacy stack identity changed")
			}
			if m.validEnvironment("prod") {
				t.Fatal("production restriction removed")
			}
			if cli.spec.buildRoot("checkout") != "checkout" {
				t.Fatal("root build moved")
			}
			// No traversal of existing source trees (which may contain links or generated files).
			if digest, err := cli.deploymentDigest(context.Background(), "nonexistent-checkout"); err != nil || digest != "" {
				t.Fatal("new digest required for legacy manifest", err)
			}
			expected := []string{"pulumi", "preview", "--stack", "test-dev", "--save-plan", "plan", "--event-log", "events", "--non-interactive"}
			if !reflect.DeepEqual(cli.previewArgs("test-dev", "plan", "events"), expected) {
				t.Fatal("legacy preview invocation changed")
			}
			cli.spec.RefreshBeforePreview = true
			if !strings.Contains(strings.Join(cli.previewArgs("test-dev", "plan", "events"), " "), "--refresh") {
				t.Fatal("explicit refresh ignored")
			}
		})
	}
}
