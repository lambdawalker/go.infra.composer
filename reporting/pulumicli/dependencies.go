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
			Goal *struct {
				resourceDependencies
				Type   string `json:"type"`
				Custom *bool  `json:"custom"`
				Parent string `json:"parent"`
			} `json:"goal"`
			Steps []string `json:"steps"`
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
	// A saved plan can be the only source of inventory on a first preview.
	for _, key := range slices.Sorted(maps.Keys(plan.ResourcePlans)) {
		entry := plan.ResourcePlans[key]
		goal := entry.Goal
		if known[key] || goal == nil || goal.Custom == nil || !inventoryResource(resource{URN: key, Type: goal.Type}) {
			continue
		}
		a := r.Artifact{Key: key, Name: name(key), Type: goal.Type, Provider: provider(goal.Type), Priority: Priority(goal.Type),
			Observation: r.Observation{Status: r.Unknown, Reason: r.Unchecked}}
		if len(entry.Steps) > 0 {
			a.PlannedAction = allowedAction(entry.Steps[0])
		}
		out = append(out, a)
		known[key] = true
	}
	for i := range out {
		if goal := plan.ResourcePlans[out[i].Key].Goal; goal != nil {
			out[i].Dependencies = goal.keys(out[i].Key, known)
			if goal.Custom != nil {
				out[i].Component = !*goal.Custom
			}
			if goal.Parent != out[i].Key && known[goal.Parent] {
				out[i].Parent = goal.Parent
			}
		}
	}
	return out, nil
}
