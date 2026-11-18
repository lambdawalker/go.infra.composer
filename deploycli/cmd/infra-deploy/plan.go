package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// destructivePlan checks planned operations, not property names or policy contents.
func destructivePlan(data []byte) (bool, error) {
	var plan struct {
		ResourcePlans map[string]struct {
			Steps []string `json:"steps"`
		} `json:"resourcePlans"`
	}
	if e := json.Unmarshal(data, &plan); e != nil {
		return false, fmt.Errorf("invalid Pulumi plan: %w", e)
	}
	if plan.ResourcePlans == nil {
		return false, fmt.Errorf("Pulumi plan has no resourcePlans; cannot inspect operations")
	}
	for _, r := range plan.ResourcePlans {
		for _, op := range r.Steps {
			if strings.Contains(op, "delete") || strings.Contains(op, "replace") {
				return true, nil
			}
		}
	}
	return false, nil
}
