// Package pulumiadapter evaluates trusted composer plans inside a Pulumi program.
package pulumiadapter

import (
	"fmt"
	"sort"

	composer "github.com/lambdawalker/go.infra.composer"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Factory is trusted Go code using provider SDK constructors or RegisterResource.
// It must expose every output declared by its composer resource and forward the
// supplied options, including dependencies and additional secret outputs.
type Factory func(*pulumi.Context, string, pulumi.Map, ...pulumi.ResourceOption) (Registered, error)

// Registry is a bounded allowlist of logical composer kinds.
type Registry map[string]Factory

// Registered retains the native SDK resource and typed outputs.
type Registered struct {
	Resource pulumi.Resource
	Outputs  map[string]pulumi.Output
}

// Result contains native Outputs, which may be unknown during preview.
type Result struct {
	Resources map[string]Registered
	Exports   map[string]pulumi.Output
}

// Execute registers a plan in the supplied context. It is not a core Executor,
// because its result contains native Pulumi Outputs rather than simulated values.
func Execute(ctx *pulumi.Context, plan *composer.Plan, registry Registry) (*Result, error) {
	if !plan.Valid() {
		return nil, fmt.Errorf("pulumi adapter requires a validated plan")
	}
	if ctx == nil {
		return nil, fmt.Errorf("pulumi adapter requires a context")
	}
	resources := plan.Resources()
	byID := make(map[string]composer.PlannedResource, len(resources))
	for _, r := range resources {
		if registry[r.Kind] == nil {
			return nil, fmt.Errorf("unsupported resource kind %q at %q", r.Kind, r.ID)
		}
		for _, field := range r.Outputs {
			switch field.Type {
			case composer.String, composer.Integer, composer.Number, composer.Boolean:
			default:
				return nil, fmt.Errorf("unsupported output type at resource %q", r.ID)
			}
		}
		byID[r.ID] = r
	}
	// Check every value before invoking any trusted constructor.
	for _, r := range resources {
		for _, v := range r.Inputs {
			if err := validateValue(v, byID); err != nil {
				return nil, err
			}
		}
	}
	for _, v := range plan.Exports() {
		if err := validateValue(v, byID); err != nil {
			return nil, err
		}
	}
	order := plan.Order()
	seen := map[string]bool{}
	for _, id := range order {
		r, ok := byID[id]
		if !ok || seen[id] {
			return nil, fmt.Errorf("invalid plan resource order")
		}
		for _, d := range r.Dependencies {
			if !seen[d.Resource] {
				return nil, fmt.Errorf("invalid dependency order at resource %q", id)
			}
		}
		seen[id] = true
	}
	if len(seen) != len(byID) {
		return nil, fmt.Errorf("incomplete plan resource order")
	}
	result := &Result{Resources: map[string]Registered{}, Exports: map[string]pulumi.Output{}}
	for _, id := range order {
		r := byID[id]
		inputs := pulumi.Map{}
		tainted := false
		for key, v := range r.Inputs {
			inputs[key] = resolve(v, result.Resources)
			tainted = tainted || v.IsSecret()
		}
		dependencies := make([]pulumi.Resource, 0, len(r.Dependencies))
		for _, d := range r.Dependencies {
			dependencies = append(dependencies, result.Resources[d.Resource].Resource)
		}
		secrets := []string{}
		for name, field := range r.Outputs {
			if field.Sensitive || tainted {
				secrets = append(secrets, name)
			}
		}
		sort.Strings(secrets)
		registered, err := registry[r.Kind](ctx, id, inputs, pulumi.DependsOn(dependencies), pulumi.AdditionalSecretOutputs(secrets))
		// Provider errors can include input values; do not copy them into diagnostics.
		if err != nil {
			return nil, fmt.Errorf("resource %q registration failed", id)
		}
		if registered.Resource == nil {
			return nil, fmt.Errorf("resource %q factory returned no resource", id)
		}
		outputs := map[string]pulumi.Output{}
		for name, field := range r.Outputs {
			output, ok := registered.Outputs[name]
			if !ok || output == nil {
				return nil, fmt.Errorf("resource %q factory omitted declared output %q", id, name)
			}
			if field.Sensitive || tainted {
				output = pulumi.ToSecret(output)
			}
			outputs[name] = output
		}
		registered.Outputs = outputs
		result.Resources[id] = registered
	}
	for name, v := range plan.Exports() {
		output := pulumi.ToOutput(resolve(v, result.Resources))
		result.Exports[name] = output
		ctx.Export(name, output)
	}
	return result, nil
}

func validateValue(v composer.Value, resources map[string]composer.PlannedResource) error {
	n := v.Node()
	switch n.Kind {
	case "literal":
	case "reference":
		r, ok := resources[n.Resource]
		if !ok {
			return fmt.Errorf("unknown resource reference %q", n.Resource)
		}
		if _, ok := r.Outputs[n.Field]; !ok {
			return fmt.Errorf("unknown output reference at resource %q", n.Resource)
		}
	case "object":
		for _, child := range n.Object {
			if err := validateValue(child, resources); err != nil {
				return err
			}
		}
	case "list":
		for _, child := range n.List {
			if err := validateValue(child, resources); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported value kind")
	}
	return nil
}

func resolve(v composer.Value, resources map[string]Registered) pulumi.Input {
	n := v.Node()
	var input pulumi.Input
	switch n.Kind {
	case "reference":
		input = resources[n.Resource].Outputs[n.Field]
	case "object":
		m := pulumi.Map{}
		for name, child := range n.Object {
			m[name] = resolve(child, resources)
		}
		input = m
	case "list":
		a := make(pulumi.Array, len(n.List))
		for i, child := range n.List {
			a[i] = resolve(child, resources)
		}
		input = a
	default:
		input = pulumi.Any(n.Literal)
	}
	if n.Secret {
		return pulumi.ToSecret(input)
	}
	return input
}
