package composer

import (
	"encoding/json"
	"fmt"
)

type PlannedResource struct {
	ID           string           `json:"id"`
	Kind         string           `json:"kind"`
	Source       string           `json:"source"`
	Inputs       map[string]Value `json:"inputs"`
	Outputs      map[string]Field `json:"outputs"`
	Dependencies []Dependency     `json:"dependencies"`
}
type Binding struct {
	Consumer string `json:"consumer"`
	Provider string `json:"provider"`
	Contract string `json:"contract"`
	Origin   string `json:"origin"`
}
type RuntimeDependency struct {
	Consumer string `json:"consumer"`
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
	Source   string `json:"source"`
}

// Plan is valid only when returned by Application.Plan; zero plans are rejected.
type Plan struct {
	name        string
	environment Environment
	resources   []PlannedResource
	order       []string
	exports     map[string]Value
	bindings    []Binding
	runtime     []RuntimeDependency
	valid       bool
}

func (p *Plan) Name() string             { return p.name }
func (p *Plan) Environment() Environment { return p.environment }
func (p *Plan) Resources() []PlannedResource {
	r := make([]PlannedResource, len(p.resources))
	for i, x := range p.resources {
		x.Inputs = copyValues(x.Inputs)
		x.Outputs = cloneFields(x.Outputs)
		x.Dependencies = append([]Dependency{}, x.Dependencies...)
		r[i] = x
	}
	return r
}
func (p *Plan) Order() []string           { return append([]string{}, p.order...) }
func (p *Plan) Exports() map[string]Value { return copyValues(p.exports) }
func (p *Plan) Bindings() []Binding       { return append([]Binding{}, p.bindings...) }
func (p *Plan) RuntimeDependencies() []RuntimeDependency {
	return append([]RuntimeDependency{}, p.runtime...)
}

// Valid identifies a plan produced by the validator, not a serialized import.
func (p *Plan) Valid() bool               { return p != nil && p.valid }
func (p Plan) String() string             { b, _ := p.MarshalJSON(); return string(b) }
func (p Plan) Format(s fmt.State, _ rune) { fmt.Fprint(s, p.String()) }
func (p Plan) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Application string              `json:"application"`
		Environment Environment         `json:"environment"`
		Resources   []PlannedResource   `json:"resources"`
		Order       []string            `json:"order"`
		Exports     map[string]Value    `json:"exports"`
		Bindings    []Binding           `json:"bindings"`
		Runtime     []RuntimeDependency `json:"runtime_dependencies"`
	}{p.name, p.environment, p.Resources(), p.Order(), p.Exports(), p.Bindings(), p.RuntimeDependencies()})
}
