package composer

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

// Type is a supported contract/output scalar type.
type Type string

const (
	String  Type = "string"
	Integer Type = "integer"
	Number  Type = "number"
	Boolean Type = "boolean"
)

type Field struct {
	Type      Type `json:"type"`
	Sensitive bool `json:"sensitive,omitempty"`
}

// Value carries literals, deferred references and sensitivity through planning.
// Its JSON and fmt representations redact sensitive content.
type Value struct {
	node                 Node
	feature, requirement string
}

// Node is an explicit trusted-adapter boundary. Literal may expose plaintext.
// Never serialize a Node into public responses, logs or previews.
type Node struct {
	Kind            string
	Literal         any
	Resource, Field string
	Secret          bool
	Object          map[string]Value
	List            []Value
}

func Literal(v any) Value { return Value{node: Node{Kind: "literal", Literal: v}} }
func ResourceRef(resource, field string) Value {
	return Value{node: Node{Kind: "reference", Resource: resource, Field: field}}
}
func RequirementRef(feature, requirement, field string) Value {
	return Value{node: Node{Kind: "requirement", Field: field}, feature: feature, requirement: requirement}
}
func Object(v map[string]Value) Value {
	return Value{node: Node{Kind: "object", Object: copyValues(v)}}
}
func List(v ...Value) Value {
	out := make([]Value, len(v))
	for i, x := range v {
		out[i] = cloneValue(x)
	}
	return Value{node: Node{Kind: "list", List: out}}
}
func Secret(v Value) Value { v = cloneValue(v); v.node.Secret = true; return v }
func (v Value) IsSecret() bool {
	if v.node.Secret {
		return true
	}
	n, err := normalizeValue(v, 0, map[visit]bool{})
	return err == nil && n.hasSecret()
}
func (v Value) hasSecret() bool {
	if v.node.Secret {
		return true
	}
	for _, x := range v.node.Object {
		if x.hasSecret() {
			return true
		}
	}
	for _, x := range v.node.List {
		if x.hasSecret() {
			return true
		}
	}
	return false
}
func (v Value) Node() Node { return cloneValue(v).node }
func cloneValue(v Value) Value {
	n := v.node
	if n.Object != nil {
		n.Object = copyValues(n.Object)
	}
	if n.List != nil {
		n.List = append([]Value(nil), n.List...)
		for i := range n.List {
			n.List[i] = cloneValue(n.List[i])
		}
	}
	// Literal containers are normalized/copied at the plan boundary, not shared.
	v.node = n
	return v
}
func copyValues(m map[string]Value) map[string]Value {
	r := make(map[string]Value, len(m))
	for k, v := range m {
		r[k] = cloneValue(v)
	}
	return r
}
func (v Value) public() any {
	if v.IsSecret() {
		return map[string]any{"secret": true, "value": "[REDACTED]"}
	}
	switch v.node.Kind {
	case "reference":
		return map[string]any{"$output": v.node.Resource + "." + v.node.Field}
	case "requirement":
		return map[string]any{"$requirement": v.feature + "." + v.requirement + "." + v.node.Field}
	case "object":
		r := map[string]any{}
		for k, x := range v.node.Object {
			r[k] = x.public()
		}
		return r
	case "list":
		r := make([]any, len(v.node.List))
		for i, x := range v.node.List {
			r[i] = x.public()
		}
		return r
	default:
		n, err := normalizeValue(v, 0, map[visit]bool{})
		if err != nil {
			return "[INVALID VALUE]"
		}
		if n.node.Kind != "literal" {
			return n.public()
		}
		return n.node.Literal
	}
}
func (v Value) MarshalJSON() ([]byte, error) { return json.Marshal(v.public()) }
func (v Value) String() string               { b, _ := v.MarshalJSON(); return string(b) }
func (v Value) Format(s fmt.State, _ rune)   { fmt.Fprint(s, v.String()) }

type visit struct {
	kind reflect.Kind
	ptr  uintptr
}

func normalizeValue(v Value, depth int, seen map[visit]bool) (Value, error) {
	if depth > 256 {
		return Value{}, fmt.Errorf("value nesting exceeds 256")
	}
	switch v.node.Kind {
	case "reference", "requirement":
		return v, nil
	case "object":
		out := make(map[string]Value, len(v.node.Object))
		for _, k := range sortedKeys(v.node.Object) {
			x, err := normalizeValue(v.node.Object[k], depth+1, seen)
			if err != nil {
				return Value{}, err
			}
			out[k] = x
		}
		v.node.Object = out
		return v, nil
	case "list":
		out := make([]Value, len(v.node.List))
		for i, x := range v.node.List {
			n, err := normalizeValue(x, depth+1, seen)
			if err != nil {
				return Value{}, err
			}
			out[i] = n
		}
		v.node.List = out
		return v, nil
	case "literal", "":
		n, err := normalizeLiteral(v.node.Literal, depth, seen)
		if err != nil {
			return Value{}, err
		}
		n.node.Secret = n.node.Secret || v.node.Secret
		return n, nil
	default:
		return Value{}, fmt.Errorf("unsupported value kind")
	}
}
func normalizeLiteral(v any, depth int, seen map[visit]bool) (Value, error) {
	if depth > 256 {
		return Value{}, fmt.Errorf("value nesting exceeds 256")
	}
	switch x := v.(type) {
	case nil, string, bool:
		return Literal(x), nil
	case Value:
		return normalizeValue(x, depth+1, seen)
	case json.Number:
		if n, e := x.Int64(); e == nil {
			return Literal(n), nil
		}
		f, e := x.Float64()
		if e != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return Value{}, fmt.Errorf("invalid numeric literal")
		}
		return Literal(f), nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Literal(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if rv.Uint() > math.MaxInt64 {
			return Value{}, fmt.Errorf("integer exceeds supported range")
		}
		return Literal(int64(rv.Uint())), nil
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return Value{}, fmt.Errorf("nonfinite numeric literal")
		}
		return Literal(f), nil
	case reflect.Map, reflect.Slice, reflect.Array:
		key := visit{kind: rv.Kind()}
		if rv.Kind() != reflect.Array {
			key.ptr = rv.Pointer()
			if seen[key] {
				return Value{}, fmt.Errorf("cyclic literal container")
			}
			seen[key] = true
			defer delete(seen, key)
		}
		if rv.Kind() == reflect.Map {
			if rv.Type().Key().Kind() != reflect.String {
				return Value{}, fmt.Errorf("object keys must be strings")
			}
			m := map[string]Value{}
			iter := rv.MapRange()
			for iter.Next() {
				n, e := normalizeLiteral(iter.Value().Interface(), depth+1, seen)
				if e != nil {
					return Value{}, e
				}
				m[iter.Key().String()] = n
			}
			return Object(m), nil
		}
		a := make([]Value, rv.Len())
		for i := range a {
			n, e := normalizeLiteral(rv.Index(i).Interface(), depth+1, seen)
			if e != nil {
				return Value{}, e
			}
			a[i] = n
		}
		return List(a...), nil
	default:
		return Value{}, fmt.Errorf("unsupported literal type")
	}
}
