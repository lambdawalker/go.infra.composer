package main

import (
	"flag"
	"slices"
	"testing"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

func TestDeploymentFormValidatesBeforeUpdatingDefaults(t *testing.T) {
	spec := fixture(t, "email").spec
	inputs := spec.inputFlags(flag.NewFlagSet("test", flag.ContinueOnError))
	f := deploymentForm{
		target:   Settings{Profile: "default", Region: "us-east-2"},
		profiles: []wizardweb.Option{{Value: "default", Label: "default"}},
		inputs:   inputs,
	}
	values := map[string]string{
		"profile": "default", "region": "us-west-2", "account": "auto", "backend": "s3://example/test",
		"feature.Sender": "mail@example.test", "feature.Recipient": "invalid", "feature.Domain": "example.test",
	}
	if issues := f.apply(values); len(issues) != 1 || issues["feature.Recipient"] == "" {
		t.Fatalf("wrong validation errors: %v", issues)
	}
	if f.target.Region != "us-east-2" || inputs[0].value != "" {
		t.Fatal("invalid submission changed the run settings")
	}

	values["feature.Recipient"] = "  recipient@example.test  "
	if issues := f.apply(values); len(issues) != 0 {
		t.Fatalf("valid values rejected: %v", issues)
	}
	dir := t.TempDir()
	if err := inputs.save(spec.InputsFile, dir); err != nil {
		t.Fatal(err)
	}
	saved, err := inputs.defaults(spec.InputsFile, dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.target.Region != "us-west-2" || f.target.Account != "" || saved["Recipient"] != "recipient@example.test" {
		t.Fatalf("accepted settings did not become defaults: %+v, %v", f.target, saved)
	}
}

func TestDeploymentFormKeepsVerifiedTargetReadOnly(t *testing.T) {
	saved := Settings{Profile: "default", Region: "us-east-2", Account: "123456789012", Backend: "s3://example/test"}
	f := deploymentForm{target: saved, saved: saved, profiles: []wizardweb.Option{{Value: "default"}}}
	for _, name := range []string{"region", "account", "backend"} {
		if !slices.ContainsFunc(f.fields(), func(field wizardweb.Field) bool { return field.Name == name && field.ReadOnly }) {
			t.Fatalf("%s should be read-only", name)
		}
	}
	if issues := f.apply(map[string]string{"profile": "default"}); len(issues) != 0 || f.target != saved {
		t.Fatalf("bound target changed: %+v, %v", f.target, issues)
	}
}
