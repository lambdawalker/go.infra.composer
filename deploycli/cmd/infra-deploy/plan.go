package main

import (
	"encoding/json"
	"maps"
	"slices"

	"fmt"
	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
	"strings"
)

type resourcePlan struct {
	Steps []string `json:"steps"`
}

func readPlan(data []byte) (map[string]resourcePlan, error) {
	var plan struct {
		ResourcePlans map[string]resourcePlan `json:"resourcePlans"`
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("invalid Pulumi plan: %w", err)
	}
	if plan.ResourcePlans == nil {
		return nil, fmt.Errorf("Pulumi plan has no resourcePlans; cannot inspect operations")
	}
	return plan.ResourcePlans, nil
}

// destructivePlan checks planned operations, not property names or policy contents.
func destructivePlan(data []byte) (bool, error) {
	resources, err := readPlan(data)
	if err != nil {
		return false, err
	}
	for _, r := range resources {
		for _, op := range r.Steps {
			if strings.Contains(op, "delete") || strings.Contains(op, "replace") {
				return true, nil
			}
		}
	}
	return false, nil
}

func (w interaction) preview(data []byte, spec manifest) error {
	resources, err := readPlan(data)
	if err != nil {
		return err
	}
	var changes []wizardweb.Change
	total := 0
	for _, resource := range slices.Sorted(maps.Keys(resources)) {
		for _, step := range resources[resource].Steps {
			if step == "same" {
				continue
			}
			total++
			if len(changes) < 100 {
				changes = append(changes, wizardweb.Change{Resource: resource, Operation: step, Feature: spec.artifactFeature(resource, "")})
			}
		}
	}
	w.browser.Preview(changes, total)
	return nil
}
