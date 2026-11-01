// Command infra-compose demonstrates planning and local execution only.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	composer "github.com/lambdawalker/go.infra.composer"
	"github.com/lambdawalker/go.infra.composer/examples/authentication"
	"github.com/lambdawalker/go.infra.composer/examples/contracts"
	"github.com/lambdawalker/go.infra.composer/examples/emailvalidation"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("infra-compose", flag.ContinueOnError)
	flags.SetOutput(stderr)
	scenario := flags.String("scenario", "integrated", "integrated, auth, email, self, mutual, long, missing, ambiguous")
	environment := flags.String("environment", "dev", "environment name")
	account := flags.String("account", "local", "target account metadata (no cloud connection)")
	region := flags.String("region", "local", "target region metadata (no cloud connection)")
	production := flags.Bool("production", false, "apply production provider policy")
	planOnly := flags.Bool("plan-only", false, "validate and print the plan without local execution")
	jsonOutput := flags.Bool("json", false, "emit sanitized JSON")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	app, err := application(*scenario)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	plan, err := app.Plan(composer.Environment{Name: *environment, Account: *account, Region: *region, Production: *production})
	if err != nil {
		return reportError(stderr, err, *jsonOutput)
	}
	var result *composer.Result
	if !*planOnly {
		result, err = composer.NewSimulator().Execute(context.Background(), plan)
		if err != nil {
			return reportError(stderr, err, *jsonOutput)
		}
	}
	if *jsonOutput {
		payload := struct {
			Mode   string           `json:"mode"`
			Plan   *composer.Plan   `json:"plan"`
			Result *composer.Result `json:"result,omitempty"`
		}{"LOCAL SIMULATION", plan, result}
		if err := json.NewEncoder(stdout).Encode(payload); err != nil {
			fmt.Fprintln(stderr, "cannot encode output:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stdout, "LOCAL SIMULATION — no cloud resources are created")
	fmt.Fprintf(stdout, "Application: %s; environment: %s; account: %s; region: %s\n", plan.Name(), plan.Environment().Name, plan.Environment().Account, plan.Environment().Region)
	fmt.Fprintln(stdout, "Bindings:")
	for _, binding := range plan.Bindings() {
		fmt.Fprintf(stdout, "  %s -> %s (%s)\n", binding.Consumer, binding.Provider, binding.Origin)
	}
	fmt.Fprintln(stdout, "Resource order:")
	for i, id := range plan.Order() {
		fmt.Fprintf(stdout, "  %d. %s\n", i+1, id)
	}
	if result != nil {
		fmt.Fprintf(stdout, "Simulated operations: %d\n", len(result.Operations))
	}
	return 0
}

func reportError(stderr io.Writer, err error, asJSON bool) int {
	if asJSON {
		var validation *composer.ValidationError
		if errors.As(err, &validation) {
			_ = json.NewEncoder(stderr).Encode(struct {
				Error             string                `json:"error"`
				Diagnostics       []composer.Diagnostic `json:"diagnostics"`
				DeploymentStarted bool                  `json:"deploymentStarted"`
			}{"validation failed", validation.Diagnostics, validation.DeploymentStarted})
		} else {
			_ = json.NewEncoder(stderr).Encode(map[string]string{"error": err.Error()})
		}
	} else {
		fmt.Fprintln(stderr, err)
	}
	return 1
}

func application(scenario string) (*composer.Application, error) {
	switch scenario {
	case "integrated":
		return composer.NewApplication("integrated", authentication.Feature(), emailvalidation.Feature()), nil
	case "auth":
		return composer.NewApplication("authentication", authentication.Feature()), nil
	case "email":
		return composer.NewApplication("emailvalidation", emailvalidation.Feature()), nil
	case "self", "mutual", "long":
		count := 1
		if scenario == "mutual" {
			count = 2
		}
		if scenario == "long" {
			count = 128
		}
		feature := &composer.Feature{ID: "cycle", Source: "CLI deliberate cycle"}
		for i := 0; i < count; i++ {
			id, next := fmt.Sprintf("r%03d", i), fmt.Sprintf("cycle.r%03d", (i+1)%count)
			feature.Resources = append(feature.Resources, composer.Resource{ID: id, Kind: "example:cycle", DependsOn: []composer.Dependency{{Resource: next, Reason: "deliberate cycle demonstration", Source: "CLI scenario " + scenario}}})
		}
		return composer.NewApplication(scenario, feature), nil
	case "missing", "ambiguous":
		consumer := &composer.Feature{ID: "consumer", Requirements: []composer.Requirement{{ID: "users", Contract: contracts.UserPool()}}}
		features := []*composer.Feature{consumer}
		if scenario == "ambiguous" {
			for _, id := range []string{"first", "second"} {
				features = append(features, &composer.Feature{ID: id, Offerings: []composer.Offering{{ID: "users", Contract: contracts.UserPool(), Values: map[string]composer.Value{"id": composer.Literal(id)}}}})
			}
		}
		return composer.NewApplication(scenario, features...), nil
	default:
		return nil, fmt.Errorf("unknown scenario %q", scenario)
	}
}
