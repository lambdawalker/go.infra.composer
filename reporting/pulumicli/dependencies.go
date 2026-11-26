package pulumicli

import (
	"encoding/json"
	"io"
	"maps"
	"slices"

	r "github.com/lambdawalker/go.infra.composer/reporting"
)

func (d resourceDependencies) keys(source string, known map[string]bool) []string {
	keys := make(map[string]struct{}, len(d.Dependencies))
	for _, target := range d.Dependencies {
		if target != source && known[target] {
			keys[target] = struct{}{}
		}
	}
	for _, dependencies := range d.PropertyDependencies {
		for _, target := range dependencies {
			if target != source && known[target] {
				keys[target] = struct{}{}
			}
		}
	}
	return slices.Sorted(maps.Keys(keys))
}

// ApplyPlanDependencies overlays recorded links with the desired links from a
// saved Pulumi plan. Preview events omit graph dependencies on some Pulumi
// versions. Resources without a goal (including deletions) keep recorded links.
func ApplyPlanDependencies(as []r.Artifact, rd io.Reader) ([]r.Artifact, error) {
	var plan struct {
		ResourcePlans map[string]struct {
			Goal *resourceDependencies `json:"goal"`
		} `json:"resourcePlans"`
	}
	if err := json.NewDecoder(rd).Decode(&plan); err != nil || plan.ResourcePlans == nil {
		return as, ErrInvalidInput
	}
	known := make(map[string]bool, len(as))
	for _, a := range as {
		known[a.Key] = true
	}
	out := append([]r.Artifact(nil), as...)
	for i := range out {
		if goal := plan.ResourcePlans[out[i].Key].Goal; goal != nil {
			out[i].Dependencies = goal.keys(out[i].Key, known)
		}
	}
	return out, nil
}
