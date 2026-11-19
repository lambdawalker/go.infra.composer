package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"text/template"
)

type composeSpec struct {
	Function     string
	DefaultInput string
	Parameters   map[string]parameterBinding
}

type parameterBinding struct {
	Source string
	Name   string
	Value  *string
}

type functionSpec struct {
	Parameters map[string]string
	Result     string
	Template   string
}

type composeFunction struct {
	parameters map[string]string
	result     string
	evaluate   func(map[string]string) (string, error)
}

type plannedInput struct {
	spec     inputSpec
	function composeFunction
	userKind string
}

func (p plannedInput) acceptsInput() bool {
	return p.spec.Compose == nil || p.userKind != ""
}

func planInputs(inputs []inputSpec, custom map[string]functionSpec) ([]plannedInput, error) {
	functions, err := composeFunctions(custom)
	if err != nil {
		return nil, err
	}
	kinds := make(map[string]string, len(inputs))
	for _, spec := range inputs {
		kinds[spec.Name] = spec.Kind
	}
	pending := make([]plannedInput, len(inputs))
	dependencies := make(map[string]map[string]bool, len(inputs))
	for i, spec := range inputs {
		pending[i] = plannedInput{spec: spec}
		if spec.Compose == nil {
			continue
		}
		function, exists := functions[spec.Compose.Function]
		if !exists {
			return nil, fmt.Errorf("input %s references unknown function %q", spec.Name, spec.Compose.Function)
		}
		if spec.Kind != function.result {
			return nil, fmt.Errorf("input %s is %s but function %s returns %s", spec.Name, spec.Kind, spec.Compose.Function, function.result)
		}
		pending[i].function = function
		deps := make(map[string]bool)
		dependencies[spec.Name] = deps
		for _, name := range slices.Sorted(maps.Keys(function.parameters)) {
			kind := function.parameters[name]
			binding, ok := spec.Compose.Parameters[name]
			if !ok {
				return nil, fmt.Errorf("input %s is missing parameter %s", spec.Name, name)
			}
			switch binding.Source {
			case "userInput":
				if binding.Name != "" || binding.Value != nil || pending[i].userKind != "" {
					return nil, fmt.Errorf("input %s has an invalid or repeated userInput binding for %s", spec.Name, name)
				}
				pending[i].userKind = kind
			case "field":
				fieldKind, exists := kinds[binding.Name]
				if !exists || binding.Value != nil {
					return nil, fmt.Errorf("input %s parameter %s has an invalid or unknown field %q", spec.Name, name, binding.Name)
				}
				if binding.Name == spec.Name || deps[binding.Name] {
					return nil, fmt.Errorf("input %s has a self-reference or repeated dependency on %s", spec.Name, binding.Name)
				}
				if kind != "text" && kind != fieldKind {
					return nil, fmt.Errorf("input %s parameter %s needs %s but field %s is %s", spec.Name, name, kind, binding.Name, fieldKind)
				}
				deps[binding.Name] = true
			case "literal":
				if binding.Name != "" || binding.Value == nil || !validInput(kind, *binding.Value) {
					return nil, fmt.Errorf("input %s parameter %s needs a valid %s literal", spec.Name, name, kind)
				}
			default:
				return nil, fmt.Errorf("input %s parameter %s has unknown source %q", spec.Name, name, binding.Source)
			}
		}
		for _, name := range slices.Sorted(maps.Keys(spec.Compose.Parameters)) {
			if _, ok := function.parameters[name]; !ok {
				return nil, fmt.Errorf("input %s has unexpected parameter %s", spec.Name, name)
			}
		}
		if def := spec.Compose.DefaultInput; def != "" && !validInput(pending[i].userKind, def) {
			return nil, fmt.Errorf("input %s has an invalid DefaultInput for its userInput binding", spec.Name)
		}
	}
	ordered := make([]plannedInput, 0, len(inputs))
	resolved := make(map[string]bool, len(inputs))
	for len(ordered) < len(pending) {
		next := -1
		for i, p := range pending {
			if resolved[p.spec.Name] {
				continue
			}
			ready := true
			for dep := range dependencies[p.spec.Name] {
				if !resolved[dep] {
					ready = false
					break
				}
			}
			if ready {
				next = i
				break
			}
		}
		if next == -1 {
			var blocked []string
			for _, p := range pending {
				if !resolved[p.spec.Name] {
					blocked = append(blocked, p.spec.Name)
				}
			}
			return nil, fmt.Errorf("cyclic input dependencies block %s", strings.Join(blocked, ", "))
		}
		p := pending[next]
		resolved[p.spec.Name] = true
		ordered = append(ordered, p)
	}
	return ordered, nil
}

func composeFunctions(custom map[string]functionSpec) (map[string]composeFunction, error) {
	functions := map[string]composeFunction{
		"emailOnDomain": {
			parameters: map[string]string{"Address": "text", "Domain": "domain"},
			result:     "email",
			evaluate:   emailOnDomain,
		},
	}
	for _, name := range slices.Sorted(maps.Keys(custom)) {
		spec := custom[name]
		if _, exists := functions[name]; exists {
			return nil, fmt.Errorf("project function %q is a built-in", name)
		}
		if !pattern(`^[A-Za-z][A-Za-z0-9_]*$`, name) || !inputKind(spec.Result) || spec.Template == "" {
			return nil, fmt.Errorf("invalid project function %q", name)
		}
		for _, param := range slices.Sorted(maps.Keys(spec.Parameters)) {
			if !pattern(`^[A-Za-z][A-Za-z0-9_]*$`, param) || !inputKind(spec.Parameters[param]) {
				return nil, fmt.Errorf("invalid parameter %q for project function %s", param, name)
			}
		}
		tmpl, err := template.New(name).Option("missingkey=error").Funcs(template.FuncMap{
			"lower": strings.ToLower,
			"trim":  strings.TrimSpace,
			"reject": func(message string) (string, error) {
				return "", fmt.Errorf("%s", message)
			},
		}).Parse(spec.Template)
		if err != nil {
			return nil, fmt.Errorf("project function %s: %w", name, err)
		}
		functions[name] = composeFunction{spec.Parameters, spec.Result, func(args map[string]string) (string, error) {
			var out strings.Builder
			err := tmpl.Execute(&out, args)
			return out.String(), err
		}}
	}
	return functions, nil
}

func emailOnDomain(args map[string]string) (string, error) {
	address, domain := args["Address"], args["Domain"]
	if i := strings.LastIndexByte(address, '@'); i < 0 {
		address += "@" + domain
	} else if !strings.EqualFold(address[i+1:], domain) {
		return "", fmt.Errorf("enter a local part or an address on %s", domain)
	}
	return address, nil
}

func (p plannedInput) resolve(raw string, fields map[string]string) (string, error) {
	if p.spec.Compose == nil {
		if !validInput(p.spec.Kind, raw) {
			return "", fmt.Errorf("invalid %s", p.spec.Label)
		}
		return raw, nil
	}
	if p.userKind != "" && !validInput(p.userKind, raw) {
		return "", fmt.Errorf("%s requires a valid %s input", p.spec.Label, p.userKind)
	}
	args := make(map[string]string, len(p.function.parameters))
	for name, binding := range p.spec.Compose.Parameters {
		switch binding.Source {
		case "userInput":
			args[name] = raw
		case "field":
			args[name] = fields[binding.Name]
		case "literal":
			args[name] = *binding.Value
		}
	}
	value, err := p.function.evaluate(args)
	if err != nil {
		return "", fmt.Errorf("%s: %w", p.spec.Label, err)
	}
	if !validInput(p.spec.Kind, value) {
		return "", fmt.Errorf("%s: %s did not produce a valid %s value", p.spec.Label, p.spec.Compose.Function, p.spec.Kind)
	}
	return value, nil
}
