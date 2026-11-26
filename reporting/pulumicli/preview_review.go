package pulumicli

import (
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

type propertyDiff struct {
	Kind      string `json:"diffKind"`
	InputDiff bool   `json:"inputDiff"`
}

// PreviewField contains the available allowlisted values for a planned change.
// Empty values indicate that a side could not be shown.
type PreviewField struct {
	Name, Before, After string
}

// PreviewReview holds displayable details for a single resource in a preview.
type PreviewReview struct {
	Properties        []PreviewField
	ReplacementFields []string
}

// ReadPreviewReview selects displayable changes from Pulumi's pre-step events.
// The caller must retain the saved plan as the source of planned operations.
func ReadPreviewReview(rd io.Reader) (map[string]PreviewReview, error) {
	reviews := make(map[string]PreviewReview)
	err := events(rd, func(e event) {
		if e.Pre == nil {
			return
		}
		m := e.Pre.Metadata
		if m.URN == "" {
			return
		}
		switch m.Op {
		case "create", "update", "replace", "create-replacement", "delete", "import-replacement":
		default:
			return
		}
		review := reviews[m.URN]
		if fields := previewProperties(m); len(fields) > 0 {
			review.Properties = fields
		}
		review.ReplacementFields = append(review.ReplacementFields, replacementFields(m)...)
		slices.Sort(review.ReplacementFields)
		review.ReplacementFields = slices.Compact(review.ReplacementFields)
		if len(review.Properties) > 0 || len(review.ReplacementFields) > 0 {
			reviews[m.URN] = review
		}
	})
	return reviews, err
}

// Some preview steps reuse the old outputs as the proposed outputs. Prefer new
// inputs after an edit and avoid reusing proposed outputs for removed inputs.
func previewProperties(m metadata) []PreviewField {
	beforeOutputs, beforeInputs := previewValues(m.Type, m.Old)
	afterOutputs, afterInputs := previewValues(m.Type, m.New)
	keys := make(map[string]bool)
	for _, values := range []map[string]string{beforeOutputs, beforeInputs, afterOutputs, afterInputs} {
		for key := range values {
			keys[key] = true
		}
	}
	var changes []PreviewField
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		diff, hasDiff := m.DetailedDiff[key]
		if m.Op != "create" && m.Op != "delete" && len(m.DetailedDiff)+len(m.Diffs) > 0 &&
			!hasDiff && !slices.Contains(m.Diffs, key) {
			continue
		}
		before := beforeOutputs[key]
		if diff.InputDiff || before == "" && (m.Old == nil || m.Old.Outputs[key] == nil) {
			before = beforeInputs[key]
		}
		after := afterInputs[key]
		if after == "" && !diff.InputDiff && diff.Kind != "delete" && diff.Kind != "delete-replace" &&
			(m.New == nil || m.New.Inputs[key] == nil) {
			after = afterOutputs[key]
		}
		if before == after {
			continue
		}
		if before == "" && (m.Old != nil || !strings.Contains(m.Op, "create")) &&
			diff.Kind != "add" && diff.Kind != "add-replace" {
			continue
		}
		if after == "" && m.Op != "delete" && diff.Kind != "delete" && diff.Kind != "delete-replace" {
			continue
		}
		changes = append(changes, PreviewField{Name: key, Before: before, After: after})
	}
	return changes
}

func previewValues(typ string, state *resource) (outputs, inputs map[string]string) {
	if state == nil {
		return nil, nil
	}
	return reporting.SafeConfigurationValues(typ, state.Outputs), reporting.SafeConfigurationValues(typ, state.Inputs)
}

func replacementFields(m metadata) []string {
	var fields []string
	for _, key := range m.Keys {
		if displayableReplacementField(key) {
			fields = append(fields, key)
		}
	}
	for key, diff := range m.DetailedDiff {
		if strings.HasSuffix(diff.Kind, "-replace") && displayableReplacementField(key) {
			fields = append(fields, key)
		}
	}
	return fields
}

// Replacement paths may contain user-supplied map keys, so only a static
// allowlist of field names is shown. Replacement values are not included here.
func displayableReplacementField(key string) bool {
	switch key {
	case "name", "functionName", "namePrefix", "packageType", "runtime", "architectures",
		"memorySize", "timeout", "reservedConcurrentExecutions", "bucket", "key",
		"hashKey", "rangeKey", "attributes", "billingMode", "readCapacity", "writeCapacity",
		"deletionProtectionEnabled", "customerMasterKeySpec", "keySpec", "keyUsage", "multiRegion",
		"domainName", "zoneId", "routingMode", "retentionInDays", "visibilityTimeoutSeconds",
		"messageRetentionSeconds", "delaySeconds", "receiveWaitTimeSeconds", "maxMessageSize":
		return true
	}
	return false
}
