package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSimulatorTypedValuesSecretsAndCopies(t *testing.T) {
	f := &Feature{ID: "F", Resources: []Resource{{ID: "first", Kind: "test:resource", Inputs: map[string]Value{"key": Secret(Literal("private-canary"))}, Outputs: map[string]Field{"id": {Type: String}, "count": {Type: Integer}, "enabled": {Type: Boolean}}}, {ID: "second", Kind: "test:resource", Inputs: map[string]Value{"upstream": Object(map[string]Value{"ids": List(ResourceRef("F.first", "id"))})}, Outputs: map[string]Field{"id": {Type: String}}}}, Offerings: []Offering{{ID: "output", Contract: Contract{ID: "resource.output", Major: 1, Fields: map[string]Field{"id": {Type: String}}}, Values: map[string]Value{"id": ResourceRef("F.second", "id")}}}}
	p, err := NewApplication("app", f).Plan(Environment{Name: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Exports()["F.output.id"].IsSecret() {
		t.Fatal("export lost taint")
	}
	result, err := NewSimulator().Execute(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Operations) != 2 {
		t.Fatal(result)
	}
	n := result.Operations[0].Outputs["count"].Node()
	if n.Literal != int64(0) || !n.Secret {
		t.Fatal(n.Kind)
	}
	for _, v := range []any{p, result} {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), "private-canary") || strings.Contains(fmt.Sprintf("%#v", v), "private-canary") {
			t.Fatal("secret leaked")
		}
	}
	result.Operations[0].Outputs["id"] = Literal("changed")
	again, err := NewSimulator().Execute(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if again.Operations[0].Outputs["id"].Node().Literal == "changed" {
		t.Fatal("execution shared state")
	}
}
func TestSimulatorCancellationAndInvalidPlan(t *testing.T) {
	f := &Feature{ID: "F", Resources: []Resource{{ID: "r", Kind: "test:resource"}}}
	p, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := NewSimulator().Execute(ctx, p)
	if err == nil || r == nil || len(r.Operations) != 0 {
		t.Fatal(r, err)
	}
	for _, p := range []*Plan{nil, {}} {
		if _, err := NewSimulator().Execute(context.Background(), p); err == nil {
			t.Fatal("invalid plan accepted")
		}
	}
}

func TestCancelledEmptyAndLiteralOnlyPlansDoNotSucceed(t *testing.T) {
	features := []*Feature{
		{ID: "Empty"},
		{ID: "Exports", Offerings: []Offering{{ID: "literal", Contract: Contract{ID: "literal", Major: 1, Fields: map[string]Field{"value": {Type: String}}}, Values: map[string]Value{"value": Literal("fixture")}}}},
	}
	for _, f := range features {
		p, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r, err := NewSimulator().Execute(ctx, p)
		if err == nil || r == nil || len(r.Operations) != 0 || len(r.Exports) != 0 {
			t.Fatalf("canceled plan reported success: %v", err)
		}
	}
}
