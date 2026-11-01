package composer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type Executor interface {
	Execute(context.Context, *Plan) (*Result, error)
}
type Result struct {
	Operations []Operation      `json:"operations"`
	Exports    map[string]Value `json:"exports"`
}
type Operation struct {
	Resource string           `json:"resource"`
	Inputs   map[string]Value `json:"inputs"`
	Outputs  map[string]Value `json:"outputs"`
}

// ExecutionError exposes safe progress. It never includes arbitrary provider text.
type ExecutionError struct {
	Resource  string
	Completed []string
	cause     error
}

func (e *ExecutionError) Error() string {
	return fmt.Sprintf("execution stopped at %s after %d completed resources; inspect the protected cause", e.Resource, len(e.Completed))
}
func (e *ExecutionError) Unwrap() error { return e.cause }

type Simulator struct{}

func NewSimulator() *Simulator { return &Simulator{} }
func (s *Simulator) Execute(ctx context.Context, p *Plan) (*Result, error) {
	if !p.Valid() {
		return nil, errors.New("executor requires a validated plan")
	}
	if ctx == nil {
		return nil, errors.New("execution context is required")
	}
	result := &Result{Operations: []Operation{}, Exports: map[string]Value{}}
	created := map[string]map[string]Value{}
	resources := map[string]PlannedResource{}
	for _, r := range p.resources {
		resources[r.ID] = r
	}
	var materialize func(Value) (Value, error)
	materialize = func(v Value) (Value, error) {
		v = cloneValue(v)
		switch v.node.Kind {
		case "reference":
			out, ok := created[v.node.Resource][v.node.Field]
			if !ok {
				return Value{}, errors.New("prerequisite output is not available")
			}
			if v.node.Secret {
				out = Secret(out)
			}
			return cloneValue(out), nil
		case "object":
			for _, k := range sortedKeys(v.node.Object) {
				out, e := materialize(v.node.Object[k])
				if e != nil {
					return Value{}, e
				}
				v.node.Object[k] = out
			}
		case "list":
			for i, x := range v.node.List {
				out, e := materialize(x)
				if e != nil {
					return Value{}, e
				}
				v.node.List[i] = out
			}
		}
		return v, nil
	}
	completed := []string{}
	stopped := func(id string, err error) (*Result, error) {
		return result, &ExecutionError{Resource: id, Completed: append([]string{}, completed...), cause: err}
	}
	if err := ctx.Err(); err != nil {
		return stopped("preflight", err)
	}
	for _, id := range p.order {
		if err := ctx.Err(); err != nil {
			return stopped(id, err)
		}
		r := resources[id]
		inputs := map[string]Value{}
		for _, k := range sortedKeys(r.Inputs) {
			v, err := materialize(r.Inputs[k])
			if err != nil {
				return stopped(id, err)
			}
			inputs[k] = v
		}
		outputs := map[string]Value{}
		for _, k := range sortedKeys(r.Outputs) {
			field := r.Outputs[k]
			var literal any
			switch field.Type {
			case String:
				literal = "local://" + p.name + "/" + p.environment.Name + "/" + id + "/" + k
			case Integer:
				literal = int64(0)
			case Number:
				literal = float64(0)
			case Boolean:
				literal = false
			}
			v := Literal(literal)
			if field.Sensitive {
				v = Secret(v)
			}
			outputs[k] = v
		}
		created[id] = copyValues(outputs)
		result.Operations = append(result.Operations, Operation{id, inputs, outputs})
		completed = append(completed, id)
	}
	for _, k := range sortedKeys(p.exports) {
		if err := ctx.Err(); err != nil {
			return stopped("exports", err)
		}
		v, err := materialize(p.exports[k])
		if err != nil {
			return stopped("exports", err)
		}
		result.Exports[k] = v
	}
	if err := ctx.Err(); err != nil {
		return stopped("completion", err)
	}
	return result, nil
}
func (r *Result) String() string {
	type plain Result
	b, _ := json.Marshal((*plain)(r))
	return string(b)
}
func (r *Result) Format(s fmt.State, _ rune) { fmt.Fprint(s, r.String()) }
