package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
	"github.com/lambdawalker/go.infra.composer/reporting/pulumicli"
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
			if destructiveOperation(op) {
				return true, nil
			}
		}
	}
	return false, nil
}

func destructiveOperation(operation string) bool {
	return strings.Contains(operation, "delete") || strings.Contains(operation, "replace")
}

func (w interaction) preview(data []byte, spec manifest, eventLog string) error {
	resources, err := readPlan(data)
	if err != nil {
		return err
	}
	var review map[string]pulumicli.PreviewReview
	if events, err := os.Open(eventLog); err == nil {
		review, _ = pulumicli.ReadPreviewReview(events)
		events.Close()
	}
	changes, total, counts := previewChanges(resources, spec, review)
	w.browser.Preview(changes, total, counts)
	return nil
}

func previewChanges(resources map[string]resourcePlan, spec manifest, review map[string]pulumicli.PreviewReview) ([]wizardweb.Change, int, map[string]int) {
	const limit = 100
	var destructive, ordinary []wizardweb.Change
	total := 0
	counts := make(map[string]int)
	for _, resource := range slices.Sorted(maps.Keys(resources)) {
		feature := spec.artifactFeature(resource, "")
		for _, step := range resources[resource].Steps {
			if step == "same" {
				continue
			}
			total++
			counts[feature]++
			group := &ordinary
			if destructiveOperation(step) {
				group = &destructive
			}
			if len(*group) >= limit {
				continue
			}
			detail := review[resource]
			change := wizardweb.Change{
				Resource: resource, Operation: step, Feature: feature,
				ReplacementFields: slices.Clone(detail.ReplacementFields),
			}
			for _, field := range detail.Properties {
				change.Properties = append(change.Properties, wizardweb.PropertyChange{
					Name: field.Name, Before: field.Before, After: field.After,
				})
			}
			*group = append(*group, change)
		}
	}
	remaining := min(limit-len(destructive), len(ordinary))
	return append(destructive, ordinary[:remaining]...), total, counts
}
