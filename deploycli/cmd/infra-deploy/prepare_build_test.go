package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func preparedFixture(t *testing.T) (deployCLI, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake tools; native build covered in CI")
	}
	root := t.TempDir()
	bin := t.TempDir()
	mock, err := os.ReadFile("testdata/tools.py")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bin, "go"), mock, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	log := filepath.Join(root, "commands.jsonl")
	t.Setenv("MOCK_LOG", log)
	for _, dir := range []string{"backend", "builder", "web", "infra"} {
		os.MkdirAll(filepath.Join(root, dir), 0700)
	}
	for _, name := range []string{"deploy.json", "backend/go.mod", "backend/source.go", "builder/go.mod", "builder/source.go", "web/input.js", "infra/Pulumi.yaml"} {
		os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0600)
	}
	cli := fixture(t, "ocr")
	cli.spec.BuildWorkingDir = "backend"
	cli.spec.PulumiDir = "infra"
	cli.spec.PrepareBuild = &prepareBuildSpec{Directory: "builder", Package: ".", Inputs: []string{"web", "builder"}, Outputs: []string{"web/dist"}}
	return cli, root, log
}
func TestPreparedBuildCacheAndBinding(t *testing.T) {
	cli, root, log := preparedFixture(t)
	ctx := context.Background()
	if _, err := cli.build(ctx, root, false); err != nil {
		t.Fatal(err)
	}
	count := func() int { b, _ := os.ReadFile(log); return strings.Count(string(b), `["go", "run",`) }
	if count() != 1 {
		t.Fatal("preparation did not run")
	}
	if _, err := cli.build(ctx, root, false); err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatal("unchanged preparation reran")
	}
	before, err := cli.deploymentDigest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "web/dist/index.html"), []byte("tampered"), 0600)
	after, err := cli.deploymentDigest(ctx, root)
	if err != nil || before == after {
		t.Fatal("output tamper not detected", err)
	}
	if _, err = cli.build(ctx, root, false); err != nil {
		t.Fatal(err)
	}
	if count() != 2 {
		t.Fatal("tampered output reused")
	}
	os.WriteFile(filepath.Join(root, "web/input.js"), []byte("changed"), 0600)
	if _, err = cli.build(ctx, root, false); err != nil {
		t.Fatal(err)
	}
	if count() != 3 {
		t.Fatal("source change reused stale output")
	}
	if _, err = cli.build(ctx, root, true); err != nil {
		t.Fatal(err)
	}
	if count() != 4 {
		t.Fatal("force rebuild ignored")
	}
	os.RemoveAll(filepath.Join(root, "web/dist"))
	t.Setenv("MOCK_PREPARE_FAIL", "1")
	if _, err = cli.build(ctx, root, false); err == nil {
		t.Fatal("preparation failure ignored")
	}
}
func TestBuildPathAndEnvironmentValidation(t *testing.T) {
	cli := fixture(t, "ocr")
	for _, path := range []string{"../backend", "/tmp/backend", "backend/../other"} {
		cli.spec.BuildWorkingDir = path
		if cli.spec.validate() == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	cli.spec.BuildWorkingDir = "backend"
	cli.spec.StackPrefix = "{project}-"
	cli.spec.GitHub.EnvironmentPrefix = ""
	if err := cli.spec.validate(); err != nil {
		t.Fatal(err)
	}
	if got := cli.spec.stackName("designer", "dev"); got != "designer-dev" {
		t.Fatal(got)
	}
	if cli.spec.validEnvironment("prod") {
		t.Fatal("test host accepts production")
	}
	cli.spec.AllowProduction = true
	if !cli.spec.validEnvironment("prod") || cli.spec.validEnvironment("../prod") {
		t.Fatal("invalid production opt-in")
	}
}
func TestContentDigestRejectsMissingEmptyAndLinks(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "empty"), 0700)
	for _, name := range []string{"empty", "missing", "../outside"} {
		if _, err := contentDigest(root, []string{name}, false); err == nil {
			t.Fatal(name)
		}
	}
	if runtime.GOOS != "windows" {
		outside := t.TempDir()
		os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600)
		os.Symlink(outside, filepath.Join(root, "linked"))
		if _, err := contentDigest(root, []string{"linked/secret"}, false); err == nil {
			t.Fatal("symlink parent allowed")
		}
	}
}
