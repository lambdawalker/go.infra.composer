// Package configuration validates configuration contracts and selects source bindings.
// It never retrieves values, provisions identities, or writes provider configuration.
package configuration

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/lambdawalker/go.infra.composer"
)

type Category string

const (
	Application Category = "application"
	Artifact    Category = "artifact"
)

type Sensitivity string

const (
	Public       Sensitivity = "public"
	Confidential Sensitivity = "confidential"
)
const (
	MissingError     = "error"
	MissingDefault   = "default"
	MissingOmit      = "omit"
	UnavailableError = "error"
)

// Property describes a logical value. Category never implies sensitivity or permissions.
// RequiredControls names controls that the application's versioned policy requires.
type Property struct {
	Name                string
	Type                composer.Type
	Category            Category
	Sensitivity         Sensitivity
	Required            bool
	RequiredControls    []string
	MaxStalenessSeconds int
}

// Consumer is one artifact's complete reader support for a logical property.
type Consumer struct {
	Artifact, Property string
	Readers            []string
}

// Source describes one complete storage/delivery arrangement, not a runtime fallback.
// Controls are application/provider policy assertions, not verified effective permissions.
// Security comparisons use strict control-set inclusion; incomparable sets fail closed.
// Lower ProviderRank and EstimatedCost are preferable after security selection.
type Source struct {
	Name, Storage, Reader, Location, Key, Scope  string
	ReaderIdentity, WriterIdentity, UpdateMethod string
	Controls                                     []string
	SecretDelivery                               bool
	MaxStalenessSeconds                          int
	ProviderRank                                 int
	CostKnown                                    bool
	EstimatedCost                                float64
}

// Binding is an application-owned mapping for one consumer. Explicit mappings have
// exactly one source; Auto selects one eligible source without runtime fallback.
// Initial seeds storage; Default applies only to a missing value, never an outage.
type Binding struct {
	Artifact, Property       string
	Auto                     bool
	Sources                  []Source
	Initial, Default         *composer.Value
	OnMissing, OnUnavailable string
}
type Declaration struct {
	Environment, PolicyVersion string
	Properties                 []Property
	Consumers                  []Consumer
	Bindings                   []Binding
}
type Diagnostic struct {
	Code, Artifact, Property, Message string
	Details                           []string
}
type ValidationError struct{ Diagnostics []Diagnostic }

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Diagnostics))
	for i, d := range e.Diagnostics {
		parts[i] = d.Code + ": " + d.Artifact + "/" + d.Property + ": " + d.Message
		if len(d.Details) > 0 {
			parts[i] += " (" + strings.Join(d.Details, "; ") + ")"
		}
	}
	return strings.Join(parts, "; ")
}

// Selection explains a static policy decision. It is not evidence of effective access.
type Selection struct {
	Artifact, Property       string
	Category                 Category
	Sensitivity              Sensitivity
	Source                   Source
	Initial, Default         *composer.Value
	OnMissing, OnUnavailable string
	Rejected                 []Rejection
}
type Rejection struct {
	Source  string
	Reasons []string
}
type Plan struct {
	environment, policyVersion string
	bindings                   []Selection
}

func (p *Plan) Environment() string   { return p.environment }
func (p *Plan) PolicyVersion() string { return p.policyVersion }
func (p *Plan) Bindings() []Selection { return cloneSelections(p.bindings) }
func (p *Plan) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Environment   string
		PolicyVersion string
		Bindings      []Selection
	}{p.environment, p.policyVersion, p.Bindings()})
}
func (p *Plan) GoString() string { return p.String() }

func (p *Plan) String() string {
	b, err := p.MarshalJSON()
	if err != nil {
		return "configuration plan (serialization unavailable)"
	}
	return string(b)
}

// Validate validates full inventory coverage and creates an owned, deterministic plan.
// Errors contain metadata only and never include initial/default values.
func Validate(d Declaration) (*Plan, error) {
	var diagnostics []Diagnostic
	add := func(code, a, p, m string) {
		diagnostics = append(diagnostics, Diagnostic{Code: code, Artifact: a, Property: p, Message: m})
	}
	if d.Environment == "" || d.PolicyVersion == "" {
		add("invalid_metadata", "", "", "environment and policy version are required")
	}
	props := map[string]Property{}
	for _, p := range d.Properties {
		if _, ok := props[p.Name]; ok {
			add("duplicate_property", "", p.Name, "property declared more than once")
		}
		props[p.Name] = p
		if p.Name == "" || (p.Category != Application && p.Category != Artifact) || (p.Sensitivity != Public && p.Sensitivity != Confidential) || !validType(p.Type) || p.MaxStalenessSeconds < 0 || !uniqueNonempty(p.RequiredControls) {
			add("invalid_property", "", p.Name, "name, type, category, sensitivity, controls, or freshness is invalid")
		}
	}
	consumers := map[string]Consumer{}
	usedProps := map[string]bool{}
	for _, c := range d.Consumers {
		k := key(c.Artifact, c.Property)
		if _, ok := consumers[k]; ok {
			add("duplicate_consumer", c.Artifact, c.Property, "consumer declared more than once")
		}
		consumers[k] = c
		usedProps[c.Property] = true
		if _, ok := props[c.Property]; !ok {
			add("unknown_property", c.Artifact, c.Property, "consumer references an undeclared property")
		}
		if c.Artifact == "" || len(c.Readers) == 0 || !uniqueNonempty(c.Readers) {
			add("invalid_consumer", c.Artifact, c.Property, "artifact and unique supported readers are required")
		}
	}
	for n := range props {
		if !usedProps[n] {
			add("configuration_gap", "", n, "property has no artifact consumer")
		}
	}
	bindings := map[string]Binding{}
	for _, b := range d.Bindings {
		k := key(b.Artifact, b.Property)
		if _, ok := bindings[k]; ok {
			add("duplicate_binding", b.Artifact, b.Property, "binding declared more than once")
		}
		bindings[k] = b
		if _, ok := consumers[k]; !ok {
			add("unknown_binding", b.Artifact, b.Property, "binding does not match a declared consumer")
		}
	}
	plan := &Plan{environment: d.Environment, policyVersion: d.PolicyVersion}
	envOwners := map[string]string{}
	writers := map[string]string{}
	owners := map[string]string{}
	seeds := map[string]composer.Value{}
	keys := make([]string, 0, len(consumers))
	for k := range consumers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := consumers[k]
		p, ok := props[c.Property]
		if !ok {
			continue
		}
		b, ok := bindings[k]
		if !ok {
			add("configuration_gap", c.Artifact, c.Property, "no application source binding supplied")
			continue
		}
		if b.OnUnavailable != UnavailableError {
			add("invalid_unavailable_policy", c.Artifact, c.Property, "unavailable sources must fail; missing-value defaults do not cover outages")
		}
		if b.OnMissing != MissingError && b.OnMissing != MissingDefault && b.OnMissing != MissingOmit || b.OnMissing == MissingDefault && b.Default == nil || b.OnMissing != MissingDefault && b.Default != nil || b.OnMissing == MissingOmit && p.Required {
			add("invalid_default_policy", c.Artifact, c.Property, "missing-value policy and default do not agree with the property")
		}
		for _, v := range []*composer.Value{b.Initial, b.Default} {
			if v != nil && !validValue(*v, p) {
				add("invalid_value", c.Artifact, c.Property, "initial/default must have the declared scalar type and protect sensitive material")
			}
		}
		if !b.Auto && len(b.Sources) > 1 {
			add("source_fallback", c.Artifact, c.Property, "explicit bindings accept exactly one source; runtime fallback is unsupported")
			continue
		}
		var eligible []Source
		var rejected []Rejection
		names := map[string]bool{}
		for _, s := range b.Sources {
			if names[s.Name] {
				add("duplicate_source", c.Artifact, c.Property, "source names must be unique")
			}
			names[s.Name] = true
			securityProperty := p
			if b.Initial != nil && b.Initial.Node().Secret || b.Default != nil && b.Default.Node().Secret {
				securityProperty.Sensitivity = Confidential
			}
			reasons := eligibility(d.Environment, securityProperty, c, s)
			if len(reasons) > 0 {
				rejected = append(rejected, Rejection{s.Name, reasons})
			} else {
				eligible = append(eligible, s)
			}
		}
		if len(eligible) == 0 {
			add("no_eligible_source", c.Artifact, c.Property, "no source meets reader, security, identity, scope, or freshness requirements")
			var details []string
			for _, rejection := range rejected {
				details = append(details, rejection.Source+": "+strings.Join(rejection.Reasons, ", "))
			}
			sort.Strings(details)
			diagnostics[len(diagnostics)-1].Details = details
			continue
		}
		selected, ok := selectSource(eligible)
		if !ok {
			add("ambiguous_source", c.Artifact, c.Property, "eligible sources have incomparable security, tied priorities, or unknown relative costs; bind explicitly")
			continue
		}
		for _, s := range eligible {
			if s.Name != selected.Name {
				rejected = append(rejected, Rejection{s.Name, []string{selectionReason(selected, s)}})
			}
		}
		if selected.Storage == "environment" {
			ek := key(c.Artifact, selected.Key)
			if owner, ok := envOwners[ek]; ok && owner != k {
				add("environment_collision", c.Artifact, c.Property, "environment key has multiple logical owners")
			}
			envOwners[ek] = k
		}
		wk := key(selected.Storage, key(selected.Scope, key(selected.Location, selected.Key)))
		if owner, ok := owners[wk]; ok && owner != c.Property {
			add("source_collision", c.Artifact, c.Property, "source location has multiple logical property owners")
		}
		owners[wk] = c.Property
		if b.Initial != nil {
			if seed, ok := seeds[wk]; ok && !reflect.DeepEqual(seed.Node(), b.Initial.Node()) {
				add("conflicting_initial", c.Artifact, c.Property, "shared source has inconsistent initialization values")
			}
			seeds[wk] = *b.Initial
		}
		if owner, ok := writers[wk]; ok && owner != selected.WriterIdentity {
			add("conflicting_writer", c.Artifact, c.Property, "source location has conflicting authoritative writers")
		}
		writers[wk] = selected.WriterIdentity
		sort.Slice(rejected, func(i, j int) bool { return rejected[i].Source < rejected[j].Source })
		plan.bindings = append(plan.bindings, Selection{c.Artifact, c.Property, p.Category, p.Sensitivity, selected, b.Initial, b.Default, b.OnMissing, b.OnUnavailable, rejected})
	}
	if len(diagnostics) > 0 {
		sort.SliceStable(diagnostics, func(i, j int) bool {
			a, b := diagnostics[i], diagnostics[j]
			return a.Artifact+"/"+a.Property+"/"+a.Code < b.Artifact+"/"+b.Property+"/"+b.Code
		})
		return nil, &ValidationError{diagnostics}
	}
	plan.bindings = cloneSelections(plan.bindings)
	return plan, nil
}
func key(a, b string) string { return fmt.Sprintf("%d:%s%s", len(a), a, b) }
func validType(t composer.Type) bool {
	return t == composer.String || t == composer.Integer || t == composer.Number || t == composer.Boolean
}
func uniqueNonempty(ss []string) bool {
	seen := map[string]bool{}
	for _, s := range ss {
		if s == "" || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
func eligibility(env string, p Property, c Consumer, s Source) []string {
	var r []string
	if s.Name == "" || s.Storage == "" || s.Reader == "" || s.Location == "" || s.Key == "" || s.ReaderIdentity == "" || s.WriterIdentity == "" || s.UpdateMethod == "" {
		r = append(r, "incomplete source metadata")
	}
	if s.Scope != env {
		r = append(r, "source scope does not match environment")
	}
	if !contains(c.Readers, s.Reader) {
		r = append(r, "unsupported reader")
	}
	if !uniqueNonempty(s.Controls) {
		r = append(r, "invalid control declarations")
	}
	for _, control := range p.RequiredControls {
		if !contains(s.Controls, control) {
			r = append(r, "required security control absent")
		}
	}
	if p.Sensitivity == Confidential && (!s.SecretDelivery || s.Storage == "environment" || s.Reader == "env" || !contains(s.Controls, "encrypted") || !contains(s.Controls, "scoped-access")) {
		r = append(r, "sensitive values require encrypted scoped secret delivery")
	}
	if s.MaxStalenessSeconds < 0 || s.Storage != "environment" && s.MaxStalenessSeconds <= 0 || p.MaxStalenessSeconds > 0 && (s.MaxStalenessSeconds <= 0 || s.MaxStalenessSeconds > p.MaxStalenessSeconds) {
		r = append(r, "invalid or incompatible staleness")
	}
	if s.ProviderRank < 0 || s.EstimatedCost < 0 || math.IsNaN(s.EstimatedCost) || math.IsInf(s.EstimatedCost, 0) {
		r = append(r, "invalid recommendation or cost")
	}
	return r
}
func selectionReason(selected, rejected Source) string {
	if dominates(selected, rejected) {
		return "weaker security controls"
	}
	if selected.ProviderRank < rejected.ProviderRank {
		return "lower provider recommendation priority"
	}
	return "higher estimated operating cost"
}
func sameControls(a, b Source) bool {
	if len(a.Controls) != len(b.Controls) {
		return false
	}
	for _, s := range a.Controls {
		if !contains(b.Controls, s) {
			return false
		}
	}
	return a.SecretDelivery == b.SecretDelivery
}
func dominates(a, b Source) bool {
	if b.SecretDelivery && !a.SecretDelivery {
		return false
	}
	for _, s := range b.Controls {
		if !contains(a.Controls, s) {
			return false
		}
	}
	return !sameControls(a, b)
}
func selectSource(ss []Source) (Source, bool) {
	var strongest []Source
	for _, a := range ss {
		dominated := false
		for _, b := range ss {
			if dominates(b, a) {
				dominated = true
				break
			}
		}
		if !dominated {
			strongest = append(strongest, a)
		}
	}
	for _, s := range strongest {
		if !sameControls(s, strongest[0]) {
			return Source{}, false
		}
	}
	rank := strongest[0].ProviderRank
	for _, s := range strongest {
		if s.ProviderRank < rank {
			rank = s.ProviderRank
		}
	}
	var ranked []Source
	for _, s := range strongest {
		if s.ProviderRank == rank {
			ranked = append(ranked, s)
		}
	}
	if len(ranked) == 1 {
		return ranked[0], true
	}
	for _, s := range ranked {
		if !s.CostKnown {
			return Source{}, false
		}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].EstimatedCost < ranked[j].EstimatedCost })
	if ranked[0].EstimatedCost == ranked[1].EstimatedCost {
		return Source{}, false
	}
	return ranked[0], true
}
func validValue(v composer.Value, p Property) bool {
	n := v.Node()
	if p.Sensitivity == Confidential && !n.Secret {
		return false
	}
	if n.Kind == "reference" {
		return n.Resource != "" && n.Field != ""
	}
	if n.Kind != "literal" {
		return false
	}
	switch p.Type {
	case composer.String:
		_, ok := n.Literal.(string)
		return ok
	case composer.Boolean:
		_, ok := n.Literal.(bool)
		return ok
	case composer.Integer, composer.Number:
		switch x := n.Literal.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return true
		case float64:
			return !math.IsNaN(x) && !math.IsInf(x, 0) && (p.Type == composer.Number || math.Trunc(x) == x)
		case float32:
			return !math.IsNaN(float64(x)) && !math.IsInf(float64(x), 0) && (p.Type == composer.Number || math.Trunc(float64(x)) == float64(x))
		}
	}
	return false
}
func cloneValue(v *composer.Value) *composer.Value {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func cloneSelections(ss []Selection) []Selection {
	out := make([]Selection, len(ss))
	for i, s := range ss {
		s.Source.Controls = append([]string(nil), s.Source.Controls...)
		s.Initial = cloneValue(s.Initial)
		s.Default = cloneValue(s.Default)
		s.Rejected = append([]Rejection(nil), s.Rejected...)
		for j := range s.Rejected {
			s.Rejected[j].Reasons = append([]string(nil), s.Rejected[j].Reasons...)
		}
		out[i] = s
	}
	return out
}
