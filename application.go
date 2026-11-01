package composer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type injection struct{ provider, offering string }
type Application struct {
	mu               sync.Mutex
	name             string
	features         []*Feature
	injections       map[string]injection
	defaults, frozen bool
}

func NewApplication(name string, features ...*Feature) *Application {
	return &Application{name: name, features: append([]*Feature(nil), features...), injections: map[string]injection{}, defaults: true}
}
func (a *Application) Inject(feature, requirement, provider, offering string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.frozen {
		return invalid("IC_FROZEN_DECLARATION", "The application has already been planned.", "application", "Create a new application to change bindings.")
	}
	key := feature + "." + requirement
	if _, ok := a.injections[key]; ok {
		return invalid("IC_DUPLICATE_BINDING", key+" already has an explicit binding.", key, "Bind each requirement once.")
	}
	var consumer, producer *Feature
	for _, f := range a.features {
		if f != nil && f.ID == feature {
			consumer = f
		}
		if f != nil && f.ID == provider {
			producer = f
		}
	}
	if consumer == nil || producer == nil {
		return invalid("IC_UNKNOWN_FEATURE", "Explicit binding names an unregistered feature.", key, "Register both feature instances in the application.")
	}
	found := false
	for _, r := range consumer.Requirements {
		if r.ID == requirement {
			found = true
		}
	}
	if !found {
		return invalid("IC_UNKNOWN_REQUIREMENT", "Explicit binding names an unknown requirement.", key, "Declare the requirement before injection.")
	}
	found = false
	for _, o := range producer.Offerings {
		if o.ID == offering {
			found = true
		}
	}
	if !found {
		return invalid("IC_UNKNOWN_OFFERING", "Explicit binding names an unknown offering.", key, "Select a declared offering.")
	}
	a.injections[key] = injection{provider, offering}
	return nil
}
func (a *Application) DisableDefaults() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.frozen {
		return invalid("IC_FROZEN_DECLARATION", "The application has already been planned.", "application", "Create a new application to change default policy.")
	}
	a.defaults = false
	return nil
}

type ownedOffering struct {
	owner    string
	offering Offering
}

func (a *Application) Plan(env Environment) (*Plan, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ds []Diagnostic
	addError := func(code, msg, src, fix string) { ds = append(ds, diagnostic(code, msg, src, fix)) }
	if !idPattern.MatchString(a.name) || !idPattern.MatchString(env.Name) {
		return nil, invalid("IC_INVALID_CONTEXT", "Application and environment require stable nonempty identifiers.", "application", "Use letters, digits, underscores or hyphens, starting with a letter.")
	}
	env.Production = env.IsProduction()
	fs := map[string]*Feature{}
	add := func(f *Feature) bool {
		if f == nil {
			addError("IC_INVALID_FEATURE", "A feature is nil.", "application", "Register a declared feature.")
			return false
		}
		if !idPattern.MatchString(f.ID) {
			addError("IC_INVALID_FEATURE", "A feature ID is invalid.", "application", "Use a stable identifier without dots or slashes.")
			return false
		}
		if old, ok := fs[f.ID]; ok {
			if old != f {
				addError("IC_DUPLICATE_FEATURE", "Feature "+f.ID+" has multiple owners.", source(f.Source, f.ID), "Assign each feature instance a unique ID.")
			}
			return false
		}
		fs[f.ID] = f
		return true
	}
	for _, f := range a.features {
		add(f)
	}
	if len(ds) > 0 {
		return nil, &ValidationError{Diagnostics: ds}
	}
	defaultOrigins := map[string]string{}
	offerings := func() map[string]ownedOffering {
		m := map[string]ownedOffering{}
		for _, id := range sortedKeys(fs) {
			for _, o := range fs[id].Offerings {
				m[id+"."+o.ID] = ownedOffering{id, o}
			}
		}
		return m
	}
	// Default expansion is monotonic. Re-resolve against the complete provider set.
	for {
		all := offerings()
		var additions []*Feature
		for _, id := range sortedKeys(fs) {
			f := fs[id]
			for _, r := range f.Requirements {
				key := id + "." + r.ID
				if _, ok := a.injections[key]; ok {
					continue
				}
				matches := 0
				for _, o := range all {
					if compatible(r, o.offering) {
						matches++
					}
				}
				if matches == 0 && a.defaults && r.Default != nil {
					d := r.Default
					if d.Feature == nil {
						addError("IC_INVALID_DEFAULT", key+" has no default feature.", source(r.Source, key), "Supply a declared default feature and offering.")
						continue
					}
					found := false
					for _, o := range d.Feature.Offerings {
						if o.ID == d.Offering && compatible(r, o) {
							found = true
						}
					}
					if !found {
						addError("IC_INVALID_DEFAULT", key+" has an incompatible or missing default offering.", source(r.Source, key), "Select a default satisfying the requirement contract.")
						continue
					}
					additions = append(additions, d.Feature)
					defaultOrigins[key] = d.Feature.ID + "." + d.Offering
				}
			}
		}
		changed := false
		for _, f := range additions {
			changed = add(f) || changed
		}
		if len(fs) > 10000 {
			addError("IC_DEFAULT_LIMIT", "Default expansion exceeded the feature limit.", "application", "Reduce or explicitly bind the default graph.")
		}
		if len(ds) > 0 {
			return nil, &ValidationError{Diagnostics: ds}
		}
		if !changed {
			break
		}
	}
	rs := map[string]PlannedResource{}
	all := offerings()
	reqs := map[string]Requirement{}
	validateContract := func(c Contract, src string) {
		if !contractPattern.MatchString(c.ID) || c.Major < 1 {
			addError("IC_INVALID_CONTRACT", "Contract identity or major is invalid.", src, "Use a stable capability identifier and positive major.")
		}
		for _, name := range sortedKeys(c.Fields) {
			f := c.Fields[name]
			if !idPattern.MatchString(name) || !validType(f.Type) {
				addError("IC_INVALID_CONTRACT", "A contract field name or scalar type is invalid.", src, "Declare supported named scalar fields.")
			}
		}
	}
	for _, id := range sortedKeys(fs) {
		f := fs[id]
		seenR, seenO, seenQ := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, r := range f.Resources {
			key := id + "." + r.ID
			src := source(r.Source, key)
			if seenR[r.ID] {
				addError("IC_DUPLICATE_RESOURCE", key+" has multiple owners.", src, "Give the resource one declaration.")
				continue
			}
			seenR[r.ID] = true
			if !idPattern.MatchString(r.ID) || strings.TrimSpace(r.Kind) == "" {
				addError("IC_INVALID_RESOURCE", "A resource ID or kind is invalid.", src, "Use a local resource identifier and nonempty supported kind.")
				continue
			}
			pr := PlannedResource{ID: key, Kind: r.Kind, Source: src, Inputs: map[string]Value{}, Outputs: cloneFields(r.Outputs), Dependencies: append([]Dependency{}, r.DependsOn...)}
			for _, name := range sortedKeys(r.Outputs) {
				if !idPattern.MatchString(name) || !validType(r.Outputs[name].Type) {
					addError("IC_INVALID_OUTPUT", key+" has an unsupported output declaration.", src, "Declare named supported scalar outputs.")
				}
			}
			for _, name := range sortedKeys(r.Inputs) {
				v, e := normalizeValue(r.Inputs[name], 0, map[visit]bool{})
				if e != nil {
					addError("IC_INVALID_VALUE", key+" input "+name+" is not finite acyclic JSON-compatible data.", src, "Use supported scalar/container values or declared references.")
					continue
				}
				pr.Inputs[name] = v
			}
			rs[key] = pr
		}
		for _, o := range f.Offerings {
			key := id + "." + o.ID
			src := source(o.Source, key)
			if !idPattern.MatchString(o.ID) || seenO[o.ID] {
				addError("IC_INVALID_OFFERING", key+" has an invalid or duplicate offering ID.", src, "Use a unique local offering ID.")
			}
			seenO[o.ID] = true
			validateContract(o.Contract, src)
			vals := map[string]Value{}
			for _, name := range sortedKeys(o.Values) {
				v, e := normalizeValue(o.Values[name], 0, map[visit]bool{})
				if e != nil {
					addError("IC_INVALID_VALUE", key+" contains invalid offering data.", src, "Use supported scalar values or references.")
					continue
				}
				if o.Contract.Fields[name].Sensitive {
					v = Secret(v)
				}
				vals[name] = v
			}
			o.Values = vals
			all[key] = ownedOffering{id, o}
		}
		for _, r := range f.Requirements {
			key := id + "." + r.ID
			src := source(r.Source, key)
			if !idPattern.MatchString(r.ID) || seenQ[r.ID] {
				addError("IC_INVALID_REQUIREMENT", key+" has an invalid or duplicate requirement ID.", src, "Use a unique local requirement ID.")
			}
			seenQ[r.ID] = true
			validateContract(r.Contract, src)
			reqs[key] = r
		}
	}
	if len(ds) > 0 {
		return nil, &ValidationError{Diagnostics: ds}
	}
	selected := map[string]ownedOffering{}
	var bindings []Binding
	for _, key := range sortedKeys(reqs) {
		r := reqs[key]
		src := source(r.Source, key)
		var candidates []string
		for _, okey := range sortedKeys(all) {
			if compatible(r, all[okey].offering) {
				candidates = append(candidates, okey)
			}
		}
		choice, mode := "", "automatic"
		if ex, ok := a.injections[key]; ok {
			choice = ex.provider + "." + ex.offering
			mode = "explicit"
			o, exists := all[choice]
			if !exists || !compatible(r, o.offering) {
				addError("IC_INCOMPATIBLE_PROVIDER", key+" has an incompatible explicit provider.", src, "Bind a compatible offering; explicit bindings never silently fall back.")
				continue
			}
		} else if len(candidates) > 1 {
			d := diagnostic("IC_AMBIGUOUS_PROVIDER", key+" matches multiple providers.", src, "Inject one compatible offering explicitly or refine selecting labels.")
			d.Details = append([]string{}, candidates...)
			ds = append(ds, d)
			continue
		} else if len(candidates) == 1 {
			choice = candidates[0]
			if defaultOrigins[key] == choice {
				mode = "default"
			}
		} else {
			addError("IC_MISSING_PROVIDER", key+" has no compatible provider under the default policy.", src, "Register/inject a compatible offering or enable an allowed default.")
			continue
		}
		o := all[choice]
		if env.Production && o.offering.TestOnly {
			addError("IC_TEST_PROVIDER", key+" selects test-only provider "+choice+" in production.", src, "Supply an explicitly production-capable provider.")
		}
		selected[key] = o
		bindings = append(bindings, Binding{key, choice, fmt.Sprintf("%s/v%d", r.Contract.ID, r.Contract.Major), mode})
	}
	if len(ds) > 0 {
		return nil, &ValidationError{Diagnostics: ds}
	}
	var resolve func(Value, string) (Value, error)
	var resolveActive func(Value, string, map[string]bool, int) (Value, error)
	resolve = func(v Value, src string) (Value, error) { return resolveActive(v, src, map[string]bool{}, 0) }
	resolveActive = func(v Value, src string, visited map[string]bool, depth int) (Value, error) {
		if depth > 256 {
			return Value{}, invalid("IC_INVALID_VALUE", "Resolved value nesting exceeds the supported limit.", src, "Use bounded acyclic value containers.")
		}
		tokens := []string{}
		defer func() {
			for _, token := range tokens {
				delete(visited, token)
			}
		}()
		sensitive := v.node.Secret
		for v.node.Kind == "requirement" {
			key := v.feature + "." + v.requirement
			r, exists := reqs[key]
			field, ok := r.Contract.Fields[v.node.Field]
			if !exists || !ok {
				return Value{}, invalid("IC_UNKNOWN_REQUIREMENT", "A reference names an undeclared requirement field.", src, "Reference an existing requirement field.")
			}
			token := key + "." + v.node.Field
			if visited[token] {
				return Value{}, invalid("IC_REFERENCE_CYCLE", "Capability outputs forward without a concrete value.", src, "Break forwarding with a concrete resource output or literal.")
			}
			visited[token] = true
			tokens = append(tokens, token)
			o := selected[key]
			next, ok := o.offering.Values[v.node.Field]
			if !ok {
				return Value{}, invalid("IC_INVALID_OFFERING", "A selected provider omits a required field.", source(o.offering.Source, o.owner+"."+o.offering.ID), "Supply every declared contract field.")
			}
			sensitive = sensitive || field.Sensitive || next.node.Secret
			v = next
		}
		v = cloneValue(v)
		v.node.Secret = v.node.Secret || sensitive
		switch v.node.Kind {
		case "object":
			for _, k := range sortedKeys(v.node.Object) {
				n, e := resolveActive(v.node.Object[k], src, visited, depth+1)
				if e != nil {
					return Value{}, e
				}
				v.node.Object[k] = n
			}
		case "list":
			for i, x := range v.node.List {
				n, e := resolveActive(x, src, visited, depth+1)
				if e != nil {
					return Value{}, e
				}
				v.node.List[i] = n
			}
		}
		return v, nil
	}
	valueType := func(v Value, src string) (Type, error) {
		switch v.node.Kind {
		case "reference":
			r, ok := rs[v.node.Resource]
			if !ok {
				return "", invalid("IC_UNKNOWN_RESOURCE", "A reference names unknown resource "+v.node.Resource+".", src, "Include the resource's owning feature.")
			}
			f, ok := r.Outputs[v.node.Field]
			if !ok {
				return "", invalid("IC_UNKNOWN_OUTPUT", "A resource reference names an undeclared output.", src, "Reference a declared output field.")
			}
			return f.Type, nil
		case "literal":
			switch v.node.Literal.(type) {
			case string:
				return String, nil
			case bool:
				return Boolean, nil
			case int64:
				return Integer, nil
			case float64:
				return Number, nil
			}
		}
		return "", nil
	}
	appendErr := func(err error) {
		if e, ok := err.(*ValidationError); ok {
			ds = append(ds, e.Diagnostics...)
		}
	}
	exports := map[string]Value{}
	for _, key := range sortedKeys(all) {
		o := all[key].offering
		src := source(o.Source, key)
		if len(o.Values) != len(o.Contract.Fields) {
			addError("IC_INVALID_OFFERING", key+" values do not match its contract fields.", src, "Supply exactly the declared fields.")
		}
		for _, name := range sortedKeys(o.Contract.Fields) {
			v, ok := o.Values[name]
			if !ok {
				addError("IC_INVALID_OFFERING", key+" omits contract field "+name+".", src, "Supply every contract field.")
				continue
			}
			n, e := resolve(v, src)
			if e != nil {
				appendErr(e)
				continue
			}
			typ, e := valueType(n, src)
			if e != nil {
				appendErr(e)
				continue
			}
			if typ != o.Contract.Fields[name].Type {
				addError("IC_INVALID_OFFERING", key+" has a field value incompatible with its scalar type.", src, "Supply a literal/output of the declared type.")
				continue
			}
			if o.Contract.Fields[name].Sensitive {
				n = Secret(n)
			}
			exports[key+"."+name] = n
		}
	}
	for _, id := range sortedKeys(rs) {
		r := rs[id]
		for _, key := range sortedKeys(r.Inputs) {
			v, e := resolve(r.Inputs[key], r.Source)
			if e != nil {
				appendErr(e)
				continue
			}
			r.Inputs[key] = v
			var walk func(Value, string)
			walk = func(x Value, path string) {
				if x.node.Kind == "reference" {
					if _, e := valueType(x, r.Source); e != nil {
						appendErr(e)
					} else {
						r.Dependencies = append(r.Dependencies, Dependency{x.node.Resource, "input " + path, r.Source})
					}
				}
				for _, k := range sortedKeys(x.node.Object) {
					walk(x.node.Object[k], path+"."+k)
				}
				for i, c := range x.node.List {
					walk(c, fmt.Sprintf("%s[%d]", path, i))
				}
			}
			walk(v, key)
		}
		for i, d := range r.Dependencies {
			if strings.TrimSpace(d.Reason) == "" {
				addError("IC_INVALID_DEPENDENCY", id+" has a prerequisite without a reason.", r.Source, "Explain why this resource needs its prerequisite.")
			}
			d.Source = source(d.Source, r.Source)
			r.Dependencies[i] = d
		}
		sort.SliceStable(r.Dependencies, func(i, j int) bool {
			if r.Dependencies[i].Resource == r.Dependencies[j].Resource {
				return r.Dependencies[i].Reason < r.Dependencies[j].Reason
			}
			return r.Dependencies[i].Resource < r.Dependencies[j].Resource
		})
		rs[id] = r
	}
	var runtimeDeps []RuntimeDependency
	for _, id := range sortedKeys(fs) {
		for _, u := range fs[id].Runtime {
			key := id + "." + u.Requirement
			o, ok := selected[key]
			if !ok || strings.TrimSpace(u.Reason) == "" {
				addError("IC_INVALID_RUNTIME", "A runtime relationship needs a declared requirement and reason.", source(u.Source, id), "Name a declared requirement and describe its runtime use.")
				continue
			}
			runtimeDeps = append(runtimeDeps, RuntimeDependency{key, o.owner + "." + o.offering.ID, u.Reason, source(u.Source, key)})
		}
	}
	if len(ds) > 0 {
		return nil, &ValidationError{Diagnostics: ds}
	}
	order, err := orderResources(rs)
	if err != nil {
		return nil, err
	}
	// Sensitive references also protect their originating native resource outputs.
	var markSource func(Value, bool)
	markSource = func(v Value, inherited bool) {
		protected := inherited || v.node.Secret
		if protected && v.node.Kind == "reference" {
			r := rs[v.node.Resource]
			f := r.Outputs[v.node.Field]
			f.Sensitive = true
			r.Outputs[v.node.Field] = f
			rs[v.node.Resource] = r
		}
		for _, x := range v.node.Object {
			markSource(x, protected)
		}
		for _, x := range v.node.List {
			markSource(x, protected)
		}
	}
	for _, v := range exports {
		markSource(v, false)
	}
	for _, r := range rs {
		for _, v := range r.Inputs {
			markSource(v, false)
		}
	}
	// Conservative taint: all outputs of a resource with sensitive inputs remain secret.
	var taint func(Value) Value
	taint = func(v Value) Value {
		v = cloneValue(v)
		if v.node.Kind == "reference" {
			if rs[v.node.Resource].Outputs[v.node.Field].Sensitive {
				v.node.Secret = true
			}
		}
		for k, x := range v.node.Object {
			v.node.Object[k] = taint(x)
		}
		for i, x := range v.node.List {
			v.node.List[i] = taint(x)
		}
		return v
	}
	for _, id := range order {
		r := rs[id]
		secret := false
		for k, v := range r.Inputs {
			r.Inputs[k] = taint(v)
			secret = secret || r.Inputs[k].IsSecret()
		}
		if secret {
			for k, f := range r.Outputs {
				f.Sensitive = true
				r.Outputs[k] = f
			}
		}
		rs[id] = r
	}
	for k, v := range exports {
		exports[k] = taint(v)
	}
	resources := make([]PlannedResource, 0, len(rs))
	for _, id := range sortedKeys(rs) {
		resources = append(resources, rs[id])
	}
	sort.Slice(runtimeDeps, func(i, j int) bool {
		if runtimeDeps[i].Consumer == runtimeDeps[j].Consumer {
			return runtimeDeps[i].Provider < runtimeDeps[j].Provider
		}
		return runtimeDeps[i].Consumer < runtimeDeps[j].Consumer
	})
	a.frozen = true
	return &Plan{name: a.name, environment: env, resources: resources, order: order, exports: exports, bindings: bindings, runtime: runtimeDeps, valid: true}, nil
}
func (a *Application) Deploy(ctx context.Context, env Environment, executor Executor) (*Result, error) {
	p, err := a.Plan(env)
	if err != nil {
		return nil, err
	}
	if executor == nil {
		return nil, fmt.Errorf("executor is required")
	}
	return executor.Execute(ctx, p)
}
