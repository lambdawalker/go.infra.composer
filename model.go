// Package composer resolves independently declared features into validated plans.
package composer

import (
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

type Contract struct {
	ID     string           `json:"id"`
	Major  int              `json:"major"`
	Fields map[string]Field `json:"fields"`
}
type Environment struct {
	Name       string `json:"name"`
	Account    string `json:"account"`
	Region     string `json:"region"`
	Production bool   `json:"production"`
}

func (e Environment) IsProduction() bool {
	return e.Production || strings.EqualFold(e.Name, "prod") || strings.EqualFold(e.Name, "production")
}

type Dependency struct {
	Resource string `json:"resource"`
	Reason   string `json:"reason"`
	Source   string `json:"source"`
}
type Resource struct {
	ID, Kind, Source string
	Inputs           map[string]Value
	Outputs          map[string]Field
	DependsOn        []Dependency
}
type Offering struct {
	ID, Source string
	Contract   Contract
	Values     map[string]Value
	Labels     map[string]string
	TestOnly   bool
}
type Requirement struct {
	ID, Source string
	Contract   Contract
	Labels     map[string]string
	Default    *Default
}
type Default struct {
	Feature  *Feature
	Offering string
}
type RuntimeUse struct{ Requirement, Reason, Source string }
type Feature struct {
	ID, Source   string
	Resources    []Resource
	Offerings    []Offering
	Requirements []Requirement
	Runtime      []RuntimeUse
}

// Origin captures a declaration call site for module authors.
func Origin() string {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		return "declaration"
	}
	return fmt.Sprintf("%s:%d", file, line)
}

var idPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
var contractPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)

func validType(t Type) bool { return t == String || t == Integer || t == Number || t == Boolean }
func source(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
func sortedKeys[V any](m map[string]V) []string {
	r := make([]string, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}
func cloneFields(m map[string]Field) map[string]Field {
	r := make(map[string]Field, len(m))
	for k, v := range m {
		r[k] = v
	}
	return r
}
func compatible(r Requirement, o Offering) bool {
	if r.Contract.ID != o.Contract.ID || r.Contract.Major != o.Contract.Major {
		return false
	}
	for k, f := range r.Contract.Fields {
		v, ok := o.Contract.Fields[k]
		if !ok || v.Type != f.Type {
			return false
		}
		if f.Sensitive && !v.Sensitive && !o.Values[k].IsSecret() {
			return false
		}
	}
	for k, v := range r.Labels {
		if o.Labels[k] != v {
			return false
		}
	}
	return true
}
