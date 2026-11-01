package examples_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	composer "github.com/lambdawalker/go.infra.composer"
	"github.com/lambdawalker/go.infra.composer/examples/authentication"
	"github.com/lambdawalker/go.infra.composer/examples/emailvalidation"
)

func TestFeaturesComposeWithoutCreationCycle(t *testing.T) {
	app := composer.NewApplication("independent", authentication.Feature(), emailvalidation.Feature())
	plan, err := app.Plan(composer.Environment{Name: "production"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Resources()) != 2 || len(plan.Bindings()) != 2 || len(plan.RuntimeDependencies()) != 1 {
		t.Fatalf("unexpected composition: resources %d, bindings %d, runtime %d", len(plan.Resources()), len(plan.Bindings()), len(plan.RuntimeDependencies()))
	}
	order := plan.Order()
	if len(order) != 2 || order[0] != "authentication.pool" || order[1] != "emailvalidation.validator" {
		t.Fatalf("creation order: %v", order)
	}
}

func TestStandaloneDefaultsAndExplicitApplicationBindings(t *testing.T) {
	for _, feature := range []*composer.Feature{authentication.Feature(), emailvalidation.Feature()} {
		plan, err := composer.NewApplication(feature.ID, feature).Plan(composer.Environment{Name: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Resources()) != 2 || len(plan.Bindings()) != 1 {
			t.Fatalf("%s did not include standalone default", feature.ID)
		}
	}
	app := composer.NewApplication("explicit", authentication.Feature(), emailvalidation.Feature())
	if err := app.Inject("authentication", "email-validator", "emailvalidation", "validator"); err != nil {
		t.Fatal(err)
	}
	if err := app.Inject("emailvalidation", "users", "authentication", "users"); err != nil {
		t.Fatal(err)
	}
	plan, err := app.Plan(composer.Environment{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range plan.Bindings() {
		if binding.Origin != "explicit" {
			t.Fatalf("expected explicit origin: %+v", binding)
		}
	}
}

func TestFeatureImportsAreIndependent(t *testing.T) {
	for _, name := range []string{"authentication", "emailvalidation"} {
		files, err := filepath.Glob(filepath.Join(name, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("no source for %s", name)
		}
		for _, filename := range files {
			file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range file.Imports {
				other := "authentication"
				if name == other {
					other = "emailvalidation"
				}
				if strings.Contains(spec.Path.Value, "/examples/"+other) {
					t.Fatalf("%s imports independent feature %s", filename, other)
				}
			}
		}
	}
}
