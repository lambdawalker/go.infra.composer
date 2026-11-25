package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) deployCLI {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec manifest
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if err = spec.validate(); err != nil {
		t.Fatal(err)
	}
	return deployCLI{spec}
}

func TestRejectManifestWithDifferentPulumiProject(t *testing.T) {
	cli := fixture(t, "email")
	root := t.TempDir()
	data, err := os.ReadFile("testdata/email.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "deploy.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, cli.spec.PulumiDir)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "Pulumi.yaml"), []byte("name: other-project\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readManifest(root); err == nil {
		t.Fatal("wrong Pulumi project accepted")
	}
}

func TestArtifactDescriptionsMustBeSafeAndNamed(t *testing.T) {
	for _, tc := range []struct{ name, description string }{
		{"", "Describes a resource."},
		{"testhost.app/secret", "Describes a resource."},
		{"testhost.app", ""},
		{"testhost.app", "  "},
		{"testhost.app", "First line\nSecond line"},
		{"testhost.app", strings.Repeat("x", 301)},
	} {
		m := fixture(t, "email").spec
		m.ArtifactDescriptions = map[string]string{tc.name: tc.description}
		if err := m.validate(); err == nil {
			t.Errorf("accepted artifact description %q: %q", tc.name, tc.description)
		}
	}
	m := fixture(t, "email").spec
	m.ArtifactDescriptions = map[string]string{"testhost.app-demo-domain": "Maps the public demo hostname to the HTTP API."}
	if err := m.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestReadLegacyEmailHostSettings(t *testing.T) {
	spec := fixture(t, "email").spec
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	inputs := spec.inputFlags(flags)
	dir := t.TempDir()
	path := filepath.Join(dir, spec.InputsFile)
	old := []byte(`{"Version":1,"Sender":"old@example.test","Recipient":"test@example.test","Domain":"example.test"}`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := inputs.collect(t.Context(), spec.InputsFile, dir, "dev", false, false, askInput); err != nil {
		t.Fatal(err)
	}
	vars := inputs.githubVariables()
	if vars["BENCH_SENDER"] != "old@example.test" || vars["BENCH_RECIPIENT"] != "test@example.test" || vars["BENCH_DOMAIN"] != "example.test" {
		t.Fatalf("legacy settings were not retained: %v", vars)
	}
}
