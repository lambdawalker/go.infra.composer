package pulumiadapter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	composer "github.com/lambdawalker/go.infra.composer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/internals"
)

type mockMonitor struct {
	mu    sync.Mutex
	calls []pulumi.MockResourceArgs
}

func (m *mockMonitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) { return a.Args, nil }
func (m *mockMonitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.calls = append(m.calls, a)
	m.mu.Unlock()
	out := a.Inputs.Copy()
	out["text"] = resource.NewStringProperty("native")
	out["count"] = resource.NewNumberProperty(7)
	out["enabled"] = resource.NewBoolProperty(true)
	out["ratio"] = resource.NewNumberProperty(0.5)
	return a.Name + "-id", out, nil
}

type mockState struct {
	pulumi.CustomResourceState
	Text    pulumi.StringOutput  `pulumi:"text"`
	Count   pulumi.IntOutput     `pulumi:"count"`
	Enabled pulumi.BoolOutput    `pulumi:"enabled"`
	Ratio   pulumi.Float64Output `pulumi:"ratio"`
}

func factory(ctx *pulumi.Context, name string, inputs pulumi.Map, opts ...pulumi.ResourceOption) (Registered, error) {
	state := &mockState{}
	if err := ctx.RegisterResource("example:index:Thing", name, inputs, state, opts...); err != nil {
		return Registered{}, err
	}
	return Registered{Resource: state, Outputs: map[string]pulumi.Output{"text": state.Text, "count": state.Count, "enabled": state.Enabled, "ratio": state.Ratio}}, nil
}
func makePlan(t *testing.T, resources ...composer.Resource) *composer.Plan {
	t.Helper()
	p, err := composer.NewApplication("adapter-test", &composer.Feature{ID: "Feature", Resources: resources, Offerings: []composer.Offering{{ID: "public", Contract: composer.Contract{ID: "demo", Major: 1, Fields: map[string]composer.Field{"text": {Type: composer.String}}}, Values: map[string]composer.Value{"text": composer.ResourceRef("Feature.first", "text")}}}}).Plan(composer.Environment{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func outputs() map[string]composer.Field {
	return map[string]composer.Field{"text": {Type: composer.String, Sensitive: true}, "count": {Type: composer.Integer}, "enabled": {Type: composer.Boolean}, "ratio": {Type: composer.Number}}
}
func TestNativeTypedReferencesSecretsAndDependencies(t *testing.T) {
	p := makePlan(t, composer.Resource{ID: "first", Kind: "thing", Outputs: outputs()}, composer.Resource{ID: "second", Kind: "thing", Inputs: map[string]composer.Value{"nested": composer.Object(map[string]composer.Value{"count": composer.ResourceRef("Feature.first", "count"), "ratio": composer.ResourceRef("Feature.first", "ratio"), "enabled": composer.ResourceRef("Feature.first", "enabled"), "items": composer.List(composer.ResourceRef("Feature.first", "text"), composer.Literal(true))})}, Outputs: outputs(), DependsOn: []composer.Dependency{{Resource: "Feature.first", Reason: "explicit"}}})
	mocks := &mockMonitor{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		result, err := Execute(ctx, p, Registry{"thing": factory})
		if err != nil {
			return err
		}
		if _, ok := result.Resources["Feature.first"].Outputs["count"].(pulumi.IntOutput); !ok {
			return fmt.Errorf("integer output lost SDK type")
		}
		out, err := internals.UnsafeAwaitOutput(context.Background(), result.Resources["Feature.first"].Outputs["text"])
		if err != nil {
			return err
		}
		if out.Value != "native" || !out.Secret {
			return fmt.Errorf("sensitive output not preserved: %#v", out)
		}
		second, err := internals.UnsafeAwaitOutput(context.Background(), result.Resources["Feature.second"].Outputs["count"])
		if err != nil {
			return err
		}
		if !second.Secret {
			return fmt.Errorf("input taint did not protect outputs")
		}
		if len(result.Exports) != len(p.Exports()) {
			return fmt.Errorf("plan exports omitted")
		}
		for _, exported := range result.Exports {
			resolved, err := internals.UnsafeAwaitOutput(context.Background(), exported)
			if err != nil {
				return err
			}
			if resolved.Value != "native" || !resolved.Secret {
				return fmt.Errorf("export value or secrecy lost")
			}
		}
		return nil
	}, pulumi.WithMocks("project", "stack", mocks))
	if err != nil {
		t.Fatal(err)
	}
	mocks.mu.Lock()
	defer mocks.mu.Unlock()
	var second *pulumi.MockResourceArgs
	for i := range mocks.calls {
		if mocks.calls[i].Name == "Feature.second" {
			second = &mocks.calls[i]
		}
	}
	if second == nil {
		t.Fatal("second resource not registered")
	}
	if len(second.RegisterRPC.GetDependencies()) == 0 {
		t.Fatal("explicit dependency omitted")
	}
	nested := second.Inputs["nested"].ObjectValue()
	if nested["count"].NumberValue() != 7 || nested["ratio"].NumberValue() != 0.5 || !nested["enabled"].BoolValue() {
		t.Fatalf("reference not resolved: %v", nested)
	}
	if !nested["items"].ArrayValue()[0].IsSecret() {
		t.Fatal("nested reference lost secret")
	}
}
func TestPreflightRejectsAllUnknownKindsBeforeRegistration(t *testing.T) {
	p := makePlan(t, composer.Resource{ID: "first", Kind: "thing", Outputs: outputs()}, composer.Resource{ID: "last", Kind: "unapproved", Outputs: outputs()})
	calls := 0
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := Execute(ctx, p, Registry{"thing": func(ctx *pulumi.Context, name string, inputs pulumi.Map, opts ...pulumi.ResourceOption) (Registered, error) {
			calls++
			return factory(ctx, name, inputs, opts...)
		}})
		if err == nil || !strings.Contains(err.Error(), "unsupported resource kind") {
			return fmt.Errorf("expected safe unsupported-kind error, got %v", err)
		}
		return nil
	}, pulumi.WithMocks("project", "stack", &mockMonitor{}))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("preflight registered %d resources", calls)
	}
}
func TestRejectsUnvalidatedPlan(t *testing.T) {
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		for _, p := range []*composer.Plan{nil, {}} {
			_, err := Execute(ctx, p, Registry{})
			if err == nil || !strings.Contains(err.Error(), "validated plan") {
				return fmt.Errorf("expected validated-plan rejection, got %v", err)
			}
		}
		return nil
	}, pulumi.WithMocks("project", "stack", &mockMonitor{}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestExplicitDependencyWithoutInputReference(t *testing.T) {
	plan := makePlan(t, composer.Resource{ID: "first", Kind: "thing", Outputs: outputs()}, composer.Resource{ID: "second", Kind: "thing", Outputs: outputs(), DependsOn: []composer.Dependency{{Resource: "Feature.first", Reason: "creation ordering only"}}})
	monitor := &mockMonitor{}
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error { _, err := Execute(ctx, plan, Registry{"thing": factory}); return err }, pulumi.WithMocks("project", "stack", monitor)); err != nil {
		t.Fatal(err)
	}
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	for _, call := range monitor.calls {
		if call.Name == "Feature.second" {
			dependencies := call.RegisterRPC.GetDependencies()
			if len(dependencies) != 1 || !strings.HasSuffix(dependencies[0], "::Feature.first") {
				t.Fatalf("explicit dependency lost: %v", dependencies)
			}
			if got := call.RegisterRPC.GetAdditionalSecretOutputs(); len(got) != 1 || got[0] != "text" {
				t.Fatalf("backend secret output declaration lost: %v", got)
			}
			return
		}
	}
	t.Fatal("second registration not found")
}
