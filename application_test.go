package composer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func endpointContract() Contract {
	return Contract{ID: "email.validation", Major: 1, Fields: map[string]Field{"endpoint": {Type: String}}}
}
func emailProvider(id string) (*Feature, Offering) {
	o := Offering{ID: "validation", Contract: endpointContract(), Values: map[string]Value{"endpoint": ResourceRef(id+".handler", "endpoint")}}
	return &Feature{ID: id, Resources: []Resource{{ID: "handler", Kind: "test:function", Outputs: map[string]Field{"endpoint": {Type: String}}}}, Offerings: []Offering{o}}, o
}
func authConsumer() *Feature {
	return &Feature{ID: "Auth", Requirements: []Requirement{{ID: "email", Contract: endpointContract()}}, Resources: []Resource{{ID: "handler", Kind: "test:function", Inputs: map[string]Value{"config": Object(map[string]Value{"urls": List(RequirementRef("Auth", "email", "endpoint"))})}}}}
}
func requireCode(t *testing.T, err error, code string) *ValidationError {
	t.Helper()
	var e *ValidationError
	if !errors.As(err, &e) {
		t.Fatalf("want validation %s, got %v", code, err)
	}
	for _, d := range e.Diagnostics {
		if d.Code == code {
			if d.Source == "" || d.Suggestion == "" {
				t.Fatalf("incomplete diagnostic %+v", d)
			}
			return e
		}
	}
	t.Fatalf("want %s, got %v", code, err)
	return nil
}
func TestMatchingAndInputEdges(t *testing.T) {
	e, _ := emailProvider("Email")
	a := NewApplication("backend", authConsumer(), e)
	p, err := a.Plan(Environment{Name: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Order(), []string{"Email.handler", "Auth.handler"}) {
		t.Fatal(p.Order())
	}
	if b := p.Bindings(); len(b) != 1 || b[0].Origin != "automatic" || b[0].Provider != "Email.validation" {
		t.Fatal(b)
	}
	for _, r := range p.Resources() {
		if r.ID == "Auth.handler" && (len(r.Dependencies) != 1 || !strings.Contains(r.Dependencies[0].Reason, "config")) {
			t.Fatal(r)
		}
	}
}
func TestExplicitBindingAndRejection(t *testing.T) {
	e, _ := emailProvider("Email")
	o, _ := emailProvider("Other")
	a := NewApplication("app", authConsumer(), e, o)
	if err := a.Inject("Auth", "email", "Other", "validation"); err != nil {
		t.Fatal(err)
	}
	p, err := a.Plan(Environment{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Bindings()[0].Provider != "Other.validation" || p.Bindings()[0].Origin != "explicit" {
		t.Fatal(p.Bindings())
	}
	requireCode(t, a.Inject("Auth", "email", "Email", "validation"), "IC_FROZEN_DECLARATION")
}
func TestProviderErrors(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := NewApplication("app", authConsumer()).Plan(Environment{Name: "dev"})
		requireCode(t, err, "IC_MISSING_PROVIDER")
	})
	t.Run("ambiguous", func(t *testing.T) {
		e, _ := emailProvider("Email")
		o, _ := emailProvider("Other")
		_, err := NewApplication("app", authConsumer(), e, o).Plan(Environment{Name: "dev"})
		requireCode(t, err, "IC_AMBIGUOUS_PROVIDER")
	})
	t.Run("explicit incompatible never falls back", func(t *testing.T) {
		e, _ := emailProvider("Email")
		e.Offerings[0].Contract.Major = 2
		fallback, _ := emailProvider("Fallback")
		f := authConsumer()
		f.Requirements[0].Default = &Default{Feature: fallback, Offering: "validation"}
		a := NewApplication("app", f, e)
		if err := a.Inject("Auth", "email", "Email", "validation"); err != nil {
			t.Fatal(err)
		}
		_, err := a.Plan(Environment{Name: "dev"})
		requireCode(t, err, "IC_INCOMPATIBLE_PROVIDER")
	})
	t.Run("labels", func(t *testing.T) {
		e, _ := emailProvider("Email")
		e.Offerings[0].Labels = map[string]string{"tenant": "one"}
		o, _ := emailProvider("Other")
		o.Offerings[0].Labels = map[string]string{"tenant": "two"}
		f := authConsumer()
		f.Requirements[0].Labels = map[string]string{"tenant": "one"}
		p, err := NewApplication("app", f, e, o).Plan(Environment{Name: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Bindings()[0].Provider != "Email.validation" {
			t.Fatal(p.Bindings())
		}
	})
	t.Run("duplicate injection", func(t *testing.T) {
		e, _ := emailProvider("Email")
		a := NewApplication("app", authConsumer(), e)
		_ = a.Inject("Auth", "email", "Email", "validation")
		requireCode(t, a.Inject("Auth", "email", "Email", "validation"), "IC_DUPLICATE_BINDING")
	})
}
func TestDefaultsAndProduction(t *testing.T) {
	for _, mode := range []string{"default", "explicit", "automatic"} {
		t.Run(mode, func(t *testing.T) {
			e, _ := emailProvider("Email")
			e.Offerings[0].TestOnly = true
			f := authConsumer()
			a := NewApplication("app", f)
			if mode == "default" {
				f.Requirements[0].Default = &Default{Feature: e, Offering: "validation"}
			} else {
				a = NewApplication("app", f, e)
				if mode == "explicit" {
					_ = a.Inject("Auth", "email", "Email", "validation")
				}
			}
			p, err := a.Plan(Environment{Name: "dev"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "default" && p.Bindings()[0].Origin != "default" {
				t.Fatal(p.Bindings())
			}
			_, err = a.Plan(Environment{Name: "prod"})
			requireCode(t, err, "IC_TEST_PROVIDER")
			_, err = a.Plan(Environment{Name: "live", Production: true})
			requireCode(t, err, "IC_TEST_PROVIDER")
		})
	}
	t.Run("disabled", func(t *testing.T) {
		e, _ := emailProvider("Email")
		f := authConsumer()
		f.Requirements[0].Default = &Default{Feature: e, Offering: "validation"}
		a := NewApplication("app", f)
		_ = a.DisableDefaults()
		_, err := a.Plan(Environment{Name: "dev"})
		requireCode(t, err, "IC_MISSING_PROVIDER")
	})
	t.Run("unused", func(t *testing.T) {
		e, _ := emailProvider("Email")
		d, _ := emailProvider("Default")
		f := authConsumer()
		f.Requirements[0].Default = &Default{Feature: d, Offering: "validation"}
		p, err := NewApplication("app", f, e).Plan(Environment{Name: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(p.Order(), ","), "Default") {
			t.Fatal(p.Order())
		}
	})
	t.Run("expanded ambiguity", func(t *testing.T) {
		d, _ := emailProvider("Default")
		extra, _ := emailProvider("Extra")
		extra.Offerings = append(extra.Offerings, Offering{ID: "extra", Contract: Contract{ID: "extra", Major: 1, Fields: map[string]Field{"ok": {Type: Boolean}}}, Values: map[string]Value{"ok": Literal(true)}})
		f := authConsumer()
		f.Requirements[0].Default = &Default{Feature: d, Offering: "validation"}
		f.Requirements = append(f.Requirements, Requirement{ID: "extra", Contract: extra.Offerings[1].Contract, Default: &Default{Feature: extra, Offering: "extra"}})
		_, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
		requireCode(t, err, "IC_AMBIGUOUS_PROVIDER")
	})
}
func TestCyclesAndRuntimeRelationships(t *testing.T) {
	for _, n := range []int{1, 2, 5} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f := &Feature{ID: "Cycle"}
			for i := 0; i < n; i++ {
				f.Resources = append(f.Resources, Resource{ID: fmt.Sprintf("r%d", i), Kind: "test:resource", DependsOn: []Dependency{{Resource: fmt.Sprintf("Cycle.r%d", (i+1)%n), Reason: "creation requirement", Source: "cycle.go:10"}}})
			}
			_, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
			e := requireCode(t, err, "IC_DEPENDENCY_CYCLE")
			d := e.Diagnostics[0]
			if len(d.Path) != n+1 || d.Path[0] != d.Path[len(d.Path)-1] {
				t.Fatal(d)
			}
		})
	}
	t.Run("runtime cycle allowed", func(t *testing.T) {
		e, _ := emailProvider("Email")
		f := authConsumer()
		c := Contract{ID: "identity", Major: 1, Fields: map[string]Field{"id": {Type: String}}}
		f.Resources = append(f.Resources, Resource{ID: "store", Kind: "test:table", Outputs: map[string]Field{"id": {Type: String}}})
		f.Offerings = []Offering{{ID: "identity", Contract: c, Values: map[string]Value{"id": ResourceRef("Auth.store", "id")}}}
		e.Requirements = []Requirement{{ID: "identity", Contract: c}}
		e.Resources[0].Inputs = map[string]Value{"store": RequirementRef("Email", "identity", "id")}
		f.Runtime = []RuntimeUse{{Requirement: "email", Reason: "send email"}}
		e.Runtime = []RuntimeUse{{Requirement: "identity", Reason: "record confirmation"}}
		p, err := NewApplication("app", f, e).Plan(Environment{Name: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		if len(p.RuntimeDependencies()) != 2 {
			t.Fatal(p.RuntimeDependencies())
		}
	})
}
func TestForwardingCycle(t *testing.T) {
	a := &Feature{ID: "A"}
	b := &Feature{ID: "B"}
	c := endpointContract()
	a.Requirements = []Requirement{{ID: "email", Contract: c}}
	b.Requirements = []Requirement{{ID: "email", Contract: c}}
	a.Offerings = []Offering{{ID: "validation", Contract: c, Values: map[string]Value{"endpoint": RequirementRef("A", "email", "endpoint")}}}
	b.Offerings = []Offering{{ID: "validation", Contract: c, Values: map[string]Value{"endpoint": RequirementRef("B", "email", "endpoint")}}}
	app := NewApplication("app", a, b)
	_ = app.Inject("A", "email", "B", "validation")
	_ = app.Inject("B", "email", "A", "validation")
	_, err := app.Plan(Environment{Name: "dev"})
	requireCode(t, err, "IC_REFERENCE_CYCLE")
}
func TestInvalidDeclarations(t *testing.T) {
	cases := []struct {
		name     string
		features []*Feature
		code     string
	}{
		{"duplicate feature", []*Feature{{ID: "A"}, {ID: "A"}}, "IC_DUPLICATE_FEATURE"},
		{"duplicate resource", []*Feature{{ID: "A", Resources: []Resource{{ID: "r", Kind: "x"}, {ID: "r", Kind: "x"}}}}, "IC_DUPLICATE_RESOURCE"},
		{"unknown reference", []*Feature{{ID: "A", Resources: []Resource{{ID: "r", Kind: "x", Inputs: map[string]Value{"x": ResourceRef("Missing.r", "id")}}}}}, "IC_UNKNOWN_RESOURCE"},
		{"bad type", []*Feature{{ID: "A", Offerings: []Offering{{ID: "bad", Contract: endpointContract(), Values: map[string]Value{"endpoint": Literal(5)}}}}}, "IC_INVALID_OFFERING"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewApplication("app", tt.features...).Plan(Environment{Name: "dev"})
			requireCode(t, err, tt.code)
		})
	}
	t.Run("aggregate", func(t *testing.T) {
		f := authConsumer()
		f.Requirements = append(f.Requirements, Requirement{ID: "other", Contract: Contract{ID: "other", Major: 1}})
		_, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
		e := requireCode(t, err, "IC_MISSING_PROVIDER")
		if len(e.Diagnostics) != 2 {
			t.Fatal(e)
		}
	})
}
func TestUnsafeInputIsRejectedWithoutValueLeak(t *testing.T) {
	cycle := map[string]any{}
	cycle["loop"] = cycle
	for _, v := range []any{math.NaN(), math.Inf(1), make(chan string), cycle} {
		f := &Feature{ID: "A", Resources: []Resource{{ID: "r", Kind: "x", Inputs: map[string]Value{"value": Literal(v)}}}}
		_, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
		requireCode(t, err, "IC_INVALID_VALUE")
	}
	f := &Feature{ID: "A", Resources: []Resource{{ID: "r", Kind: "x", Inputs: map[string]Value{"value": Secret(Literal("canary-never-print"))}}}}
	p, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []any{p, p.Resources(), p.Exports(), f.Resources[0].Inputs["value"]} {
		b, _ := json.Marshal(x)
		if strings.Contains(string(b), "canary-never-print") || strings.Contains(fmt.Sprintf("%v", x), "canary-never-print") {
			t.Fatalf("secret leaked in %T", x)
		}
	}
}
func TestPlanCopiesAndDeterminism(t *testing.T) {
	e, _ := emailProvider("Email")
	f := authConsumer()
	p, err := NewApplication("app", f, e).Plan(Environment{Name: "qa", Account: "123", Region: "us-east-2"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(p)
	rs := p.Resources()
	rs[0].Inputs["evil"] = Literal("changed")
	rs[0].Outputs["endpoint"] = Field{Type: Boolean}
	order := p.Order()
	order[0] = "evil"
	bs := p.Bindings()
	bs[0].Provider = "evil"
	f.Resources[0].Inputs["config"] = Literal("changed")
	after, _ := json.Marshal(p)
	if string(before) != string(after) {
		t.Fatal("snapshot mutated")
	}
	f = authConsumer()
	p2, err := NewApplication("app", e, f).Plan(Environment{Name: "qa", Account: "123", Region: "us-east-2"})
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := json.Marshal(p2)
	if string(before) != string(b2) {
		t.Fatal("input order changes plan")
	}
}
func TestLongCreationChain(t *testing.T) {
	f := &Feature{ID: "Long"}
	for i := 0; i < 2500; i++ {
		r := Resource{ID: fmt.Sprintf("r%04d", i), Kind: "test:resource"}
		if i > 0 {
			r.DependsOn = []Dependency{{Resource: fmt.Sprintf("Long.r%04d", i-1), Reason: "previous"}}
		}
		f.Resources = append(f.Resources, r)
	}
	p, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Order()) != 2500 {
		t.Fatal(len(p.Order()))
	}
}

type countingExecutor struct{ calls int }

func (e *countingExecutor) Execute(context.Context, *Plan) (*Result, error) {
	e.calls++
	return &Result{}, nil
}
func TestInvalidPlanNeverExecutes(t *testing.T) {
	e := &countingExecutor{}
	_, err := NewApplication("app", authConsumer()).Deploy(context.Background(), Environment{Name: "dev"}, e)
	requireCode(t, err, "IC_MISSING_PROVIDER")
	if e.calls != 0 {
		t.Fatal(e.calls)
	}
}
