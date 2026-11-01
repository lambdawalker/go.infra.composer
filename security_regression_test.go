package composer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"testing"
)

func TestNestedSecretLiteralCannotLoseSensitivity(t *testing.T) {
	v := Literal(Secret(Literal("WRAPPED_SECRET_CANARY")))
	if b, _ := json.Marshal(v); strings.Contains(string(b), "WRAPPED_SECRET_CANARY") {
		t.Fatalf("nested secret leaked: %s", b)
	}
	if !v.IsSecret() {
		t.Fatal("nested wrapper lost sensitivity")
	}
}
func TestCopiedPlanCannotLeakThroughFormatting(t *testing.T) {
	f := &Feature{ID: "F", Resources: []Resource{{ID: "r", Kind: "x", Inputs: map[string]Value{"key": Secret(Literal("COPIED_SECRET_CANARY"))}}}}
	p, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []any{p, *p} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if s := fmt.Sprintf(format, x); strings.Contains(s, "COPIED_SECRET_CANARY") {
				t.Fatal("plan copy exposed a secret")
			}
		}
	}
}
func TestCompositeForwardingCycleIsRejected(t *testing.T) {
	if os.Getenv("COMPOSER_CYCLE_CHILD") == "1" {
		debug.SetMaxStack(128 * 1024)
		c := Contract{ID: "loop", Major: 1, Fields: map[string]Field{"x": {Type: String}}}
		value := Object(map[string]Value{"again": RequirementRef("F", "req", "x")})
		if os.Getenv("COMPOSER_CYCLE_KIND") == "list" {
			value = List(RequirementRef("F", "req", "x"))
		}
		f := &Feature{ID: "F", Requirements: []Requirement{{ID: "req", Contract: c}}, Offerings: []Offering{{ID: "loop", Contract: c, Values: map[string]Value{"x": value}}}}
		app := NewApplication("app", f)
		_ = app.Inject("F", "req", "F", "loop")
		_, err := app.Plan(Environment{Name: "dev"})
		requireCode(t, err, "IC_REFERENCE_CYCLE")
		return
	}
	for _, kind := range []string{"object", "list"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCompositeForwardingCycleIsRejected$")
		cmd.Env = append(os.Environ(), "COMPOSER_CYCLE_CHILD=1", "COMPOSER_CYCLE_KIND="+kind)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("composite reference must fail safely instead of terminating process: %v; %.400s", err, b)
		}
	}
}

func TestSensitiveRequirementRejectsPublicProvider(t *testing.T) {
	c := Contract{ID: "secret", Major: 1, Fields: map[string]Field{"value": {Type: String, Sensitive: true}}}
	public := c
	public.Fields = map[string]Field{"value": {Type: String}}
	f := &Feature{ID: "F", Requirements: []Requirement{{ID: "req", Contract: c}}, Offerings: []Offering{{ID: "offer", Contract: public, Values: map[string]Value{"value": Literal("REQUIREMENT_CANARY")}}}}
	_, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
	requireCode(t, err, "IC_MISSING_PROVIDER")
	if strings.Contains(err.Error(), "REQUIREMENT_CANARY") {
		t.Fatal("diagnostic leak")
	}
}
func TestSensitiveOfferingProtectsSourceOutput(t *testing.T) {
	c := Contract{ID: "secret", Major: 1, Fields: map[string]Field{"value": {Type: String, Sensitive: true}}}
	f := &Feature{ID: "F", Resources: []Resource{{ID: "r", Kind: "test:resource", Outputs: map[string]Field{"value": {Type: String}}}}, Offerings: []Offering{{ID: "secret", Contract: c, Values: map[string]Value{"value": ResourceRef("F.r", "value")}}}}
	p, err := NewApplication("app", f).Plan(Environment{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Resources()[0].Outputs["value"].Sensitive {
		t.Fatal("computed secret would remain plaintext in native resource state")
	}
}
