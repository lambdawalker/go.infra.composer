package main

import (
	"fmt"
	"testing"

	"github.com/lambdawalker/go.infra.composer/reporting/pulumicli"
)

func TestDestructivePlan(t *testing.T) {
	for _, tc := range []struct {
		data       string
		want, fail bool
	}{
		{`{"resourcePlans":{"urn":{"steps":["create"],"goal":{"deleteBeforeReplace":false,"policy":"delete"}}}}`, false, false},
		{`{"resourcePlans":{"urn":{"steps":["delete"]}}}`, true, false},
		{`{"resourcePlans":{"urn":{"steps":["create-replacement","delete-replaced"]}}}`, true, false},
		{`{"resourcePlans":{}}`, false, false}, {`{}`, false, true}, {`bad`, false, true},
	} {
		got, e := destructivePlan([]byte(tc.data))
		if got != tc.want || (e != nil) != tc.fail {
			t.Fatal(tc.data, got, e)
		}
	}
}

func TestPreviewChangesPutsDestructiveOperationsBeforeDisplayLimit(t *testing.T) {
	resources := map[string]resourcePlan{
		"zz-delete":  {Steps: []string{"delete"}},
		"zz-replace": {Steps: []string{"replace"}},
		"unchanged":  {Steps: []string{"same"}},
	}
	for i := range 101 {
		resources[fmt.Sprintf("create-%03d", i)] = resourcePlan{Steps: []string{"create"}}
	}
	review := map[string]pulumicli.PreviewReview{
		"zz-replace": {ReplacementFields: []string{"name"}},
	}
	got, total, counts := previewChanges(resources, manifest{Feature: "host-app"}, review)
	if total != 103 || len(got) != 100 || counts["host-app"] != 103 {
		t.Fatalf("preview returned %d of %d changes with owner counts %v", len(got), total, counts)
	}
	if got[0].Resource != "zz-delete" || got[1].Resource != "zz-replace" ||
		len(got[1].ReplacementFields) != 1 || got[1].ReplacementFields[0] != "name" ||
		got[1].Feature != "host-app" || got[2].Resource != "create-000" {
		t.Fatalf("preview started with %+v", got[:3])
	}
}
