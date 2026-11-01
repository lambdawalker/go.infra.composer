package configuration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lambdawalker/go.infra.composer"
)

func valid() Declaration {
	return Declaration{Environment: "dev", PolicyVersion: "test-v1", Properties: []Property{{Name: "url", Type: composer.String, Category: Application, Sensitivity: Public, Required: true}}, Consumers: []Consumer{{Artifact: "handler", Property: "url", Readers: []string{"env", "remote"}}}, Bindings: []Binding{{Artifact: "handler", Property: "url", Sources: []Source{{Name: "local", Storage: "environment", Reader: "env", Location: "handler", Key: "URL", Scope: "dev", ReaderIdentity: "handler-role", WriterIdentity: "configuration-worker", UpdateMethod: "rollout", Controls: []string{"scoped-access"}}}, OnMissing: MissingError, OnUnavailable: UnavailableError}}}
}
func checkCode(t *testing.T, d Declaration, code string) {
	t.Helper()
	_, err := Validate(d)
	if err == nil || !strings.Contains(err.Error(), code) {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}
func TestCoverageAndNames(t *testing.T) {
	d := valid()
	if _, err := Validate(d); err != nil {
		t.Fatal(err)
	}
	d = valid()
	d.Bindings = nil
	checkCode(t, d, "configuration_gap")
	d = valid()
	d.Bindings[0].Property = "unknown"
	checkCode(t, d, "unknown_binding")
	d = valid()
	d.Properties = append(d.Properties, d.Properties[0])
	checkCode(t, d, "duplicate_property")
	d = valid()
	d.Consumers = append(d.Consumers, d.Consumers[0])
	checkCode(t, d, "duplicate_consumer")
	d = valid()
	d.Bindings = append(d.Bindings, d.Bindings[0])
	checkCode(t, d, "duplicate_binding")
}

func TestSharedSourceRequiresOneLogicalOwnerAndConsistentSeed(t *testing.T) {
	d := valid()
	d.Bindings[0].Sources = []Source{remote("one", "encrypted")}
	d.Properties = append(d.Properties, Property{Name: "other", Type: composer.String, Category: Application, Sensitivity: Public, Required: true})
	d.Consumers = append(d.Consumers, Consumer{Artifact: "worker", Property: "other", Readers: []string{"remote"}})
	b := d.Bindings[0]
	b.Artifact = "worker"
	b.Property = "other"
	d.Bindings = append(d.Bindings, b)
	checkCode(t, d, "source_collision")
	d.Properties = d.Properties[:1]
	d.Consumers[1].Property = "url"
	d.Bindings[1].Property = "url"
	first, second := composer.Literal("first"), composer.Literal("second")
	d.Bindings[0].Initial = &first
	d.Bindings[1].Initial = &second
	checkCode(t, d, "conflicting_initial")
	d.Bindings[1].Initial = &first
	if _, err := Validate(d); err != nil {
		t.Fatal(err)
	}
}
func TestSecurityCannotBeBypassed(t *testing.T) {
	d := valid()
	d.Properties[0].Sensitivity = Confidential
	checkCode(t, d, "no_eligible_source")
	d = valid()
	d.Properties[0].RequiredControls = []string{"encrypted"}
	checkCode(t, d, "no_eligible_source")
	d = valid()
	d.Bindings[0].Sources[0].ReaderIdentity = ""
	checkCode(t, d, "no_eligible_source")
	d = valid()
	d.Bindings[0].Sources[0].Key = ""
	checkCode(t, d, "no_eligible_source")
	d = valid()
	d.Bindings[0].Sources[0].Reader = "unsupported"
	checkCode(t, d, "no_eligible_source")
	d = valid()
	d.Bindings[0].Sources[0].MaxStalenessSeconds = -1
	checkCode(t, d, "no_eligible_source")
}
func remote(name string, controls ...string) Source {
	return Source{Name: name, Storage: "secret-store", Reader: "remote", Location: "config", Key: "url", Scope: "dev", ReaderIdentity: "handler-role", WriterIdentity: "worker", UpdateMethod: "versioned-write", Controls: controls, MaxStalenessSeconds: 30, ProviderRank: 1, CostKnown: true, EstimatedCost: 1}
}
func TestSelectionSecurityThenProviderThenCost(t *testing.T) {
	d := valid()
	d.Bindings[0].Auto = true
	a := remote("a", "scoped-access")
	b := remote("b", "scoped-access", "encrypted")
	b.ProviderRank = 9
	b.EstimatedCost = 999
	d.Bindings[0].Sources = []Source{a, b}
	p, err := Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	if p.Bindings()[0].Source.Name != "b" {
		t.Fatal("security must precede rank and cost")
	}
	a = remote("a", "encrypted")
	b = remote("b", "encrypted")
	b.ProviderRank = 2
	b.EstimatedCost = 0
	d.Bindings[0].Sources = []Source{b, a}
	p, err = Validate(d)
	if err != nil || p.Bindings()[0].Source.Name != "a" {
		t.Fatalf("rank: %v", err)
	}
	b.ProviderRank = 1
	b.EstimatedCost = .5
	d.Bindings[0].Sources = []Source{a, b}
	p, err = Validate(d)
	if err != nil || p.Bindings()[0].Source.Name != "b" {
		t.Fatalf("cost: %v", err)
	}
	b.EstimatedCost = 1
	d.Bindings[0].Sources = []Source{a, b}
	checkCode(t, d, "ambiguous_source")
	b.Controls = []string{"rotating"}
	b.ProviderRank = 0
	b.EstimatedCost = 0
	d.Bindings[0].Sources = []Source{a, b}
	checkCode(t, d, "ambiguous_source")
}
func TestDefaultsAndUnavailableAreDistinct(t *testing.T) {
	d := valid()
	v := composer.Literal("seed")
	d.Bindings[0].Initial = &v
	p, err := Validate(d)
	if err != nil || p.Bindings()[0].Default != nil {
		t.Fatalf("initial is distinct from default: %v", err)
	}
	d = valid()
	d.Bindings[0].OnMissing = MissingDefault
	checkCode(t, d, "invalid_default_policy")
	d = valid()
	d.Bindings[0].OnUnavailable = "use-default"
	checkCode(t, d, "invalid_unavailable_policy")
	d = valid()
	d.Bindings[0].Sources = append(d.Bindings[0].Sources, remote("fallback"))
	checkCode(t, d, "source_fallback")
}
func TestSecretRedactionAndCopies(t *testing.T) {
	d := valid()
	d.Properties[0].Sensitivity = Confidential
	s := remote("secret", "encrypted", "scoped-access")
	s.SecretDelivery = true
	d.Bindings[0].Sources = []Source{s}
	secret := composer.Secret(composer.Literal("private-token-xyz"))
	d.Bindings[0].Initial = &secret
	p, err := Validate(d)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-token") {
		t.Fatal("secret leaked")
	}
	d.Bindings[0].Sources[0].Controls[0] = "mutated"
	snapshot := p.Bindings()
	snapshot[0].Source.Controls[0] = "also-mutated"
	if p.Bindings()[0].Source.Controls[0] != "encrypted" {
		t.Fatal("plan aliases declarations or accessors")
	}
}
func TestMetadataCollisionsAndValues(t *testing.T) {
	d := valid()
	d.Properties[0].Category = "other"
	checkCode(t, d, "invalid_property")
	d = valid()
	d.Properties[0].Sensitivity = ""
	checkCode(t, d, "invalid_property")
	d = valid()
	d.Properties[0].MaxStalenessSeconds = 10
	d.Bindings[0].Sources = []Source{remote("stale", "encrypted")}
	checkCode(t, d, "no_eligible_source")
	d = valid()
	v := composer.Literal(3)
	d.Bindings[0].Default = &v
	d.Bindings[0].OnMissing = MissingDefault
	checkCode(t, d, "invalid_value")
	d = valid()
	d.Properties = append(d.Properties, Property{Name: "other", Type: composer.String, Category: Artifact, Sensitivity: Public, Required: true})
	d.Consumers = append(d.Consumers, Consumer{Artifact: "handler", Property: "other", Readers: []string{"env"}})
	b := d.Bindings[0]
	b.Property = "other"
	d.Bindings = append(d.Bindings, b)
	checkCode(t, d, "environment_collision")
}

func TestUnknownCostAndDeclarationOrder(t *testing.T) {
	d := valid()
	d.Bindings[0].Auto = true
	a := remote("a", "encrypted")
	b := remote("b", "encrypted")
	a.CostKnown = false
	b.EstimatedCost = 0
	d.Bindings[0].Sources = []Source{a, b}
	checkCode(t, d, "ambiguous_source")
	a.CostKnown = true
	a.EstimatedCost = 5
	for _, sources := range [][]Source{{a, b}, {b, a}} {
		d.Bindings[0].Sources = sources
		p, err := Validate(d)
		if err != nil || p.Bindings()[0].Source.Name != "b" {
			t.Fatalf("order-dependent selection: %v", err)
		}
	}
}
func TestConflictingWriterAndProtectedDefaults(t *testing.T) {
	d := valid()
	d.Properties = append(d.Properties, Property{Name: "other", Type: composer.String, Category: Artifact, Sensitivity: Public, Required: true})
	d.Consumers = append(d.Consumers, Consumer{Artifact: "worker", Property: "other", Readers: []string{"remote"}})
	d.Bindings[0].Sources = []Source{remote("one", "encrypted")}
	b := d.Bindings[0]
	b.Artifact = "worker"
	b.Property = "other"
	b.Sources = []Source{remote("two", "encrypted")}
	b.Sources[0].WriterIdentity = "different"
	d.Bindings = append(d.Bindings, b)
	checkCode(t, d, "conflicting_writer")
	d = valid()
	d.Properties[0].Sensitivity = Confidential
	s := remote("secret", "encrypted", "scoped-access")
	s.SecretDelivery = true
	d.Bindings[0].Sources = []Source{s}
	v := composer.Literal("do-not-print-this")
	d.Bindings[0].Default = &v
	d.Bindings[0].OnMissing = MissingDefault
	_, err := Validate(d)
	if err == nil || strings.Contains(err.Error(), "do-not-print-this") {
		t.Fatalf("unsafe validation: %v", err)
	}
	v = composer.Secret(v)
	d.Bindings[0].Default = &v
	if _, err := Validate(d); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedValueCannotUsePlaintextEnvironment(t *testing.T) {
	d := valid()
	v := composer.Secret(composer.Literal("secret"))
	d.Bindings[0].Initial = &v
	checkCode(t, d, "no_eligible_source")
}
