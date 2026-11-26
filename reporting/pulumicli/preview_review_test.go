package pulumicli

import (
	"reflect"
	"strings"
	"testing"
)

func TestPreviewReviewUsesSafeBeforeAndAfterValues(t *testing.T) {
	eventLog := strings.Join([]string{
		`{"resourcePreEvent":{"metadata":{"urn":"lambda","type":"aws:lambda/function:Function","op":"update",` +
			`"diffs":["memorySize","timeout","environment"],` +
			`"old":{"outputs":{"memorySize":512,"timeout":15,"reservedConcurrentExecutions":0,"environment":{"PASSWORD":"secret-old"}}},` +
			`"new":{"inputs":{"memorySize":1024,"timeout":"[secret]","reservedConcurrentExecutions":1,"environment":{"PASSWORD":"secret-new"}},"outputs":{"memorySize":512,"timeout":15}}}}}`,
		`{"resourcePreEvent":{"metadata":{"urn":"table","type":"aws:dynamodb/table:Table","op":"replace",` +
			`"keys":["hashKey","attributes[0].customer-id"],` +
			`"detailedDiff":{"billingMode":{"diffKind":"update","inputDiff":true},"hashKey":{"diffKind":"update-replace"},"rangeKey":{"diffKind":"add-replace"}},` +
			`"old":{"inputs":{"billingMode":"PROVISIONED","hashKey":"old id"},"outputs":{"billingMode":"PAY_PER_REQUEST"}},` +
			`"new":{"inputs":{"billingMode":"PAY_PER_REQUEST","hashKey":"new id"}}}}}`,
		`{"resourcePreEvent":{"metadata":{"urn":"table","type":"aws:dynamodb/table:Table","op":"delete-replaced","old":{"inputs":{"readCapacity":10}}}}}`,
		`{"resourcePreEvent":{"metadata":{"urn":"queue","type":"aws:sqs/queue:Queue","op":"create","new":{"inputs":{"visibilityTimeoutSeconds":30,"maxMessageSize":3.5}}}}}`,
		`{"resourcePreEvent":{"metadata":{"urn":"logs","type":"aws:cloudwatch/logGroup:LogGroup","op":"delete","old":{"outputs":{"retentionInDays":7}}}}}`,
	}, "\n")
	got, err := ReadPreviewReview(strings.NewReader(eventLog))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]PreviewReview{
		"lambda": {Properties: []PreviewField{{Name: "memorySize", Before: "512", After: "1024"}}},
		"table": {
			Properties:        []PreviewField{{Name: "billingMode", Before: "PROVISIONED", After: "PAY_PER_REQUEST"}},
			ReplacementFields: []string{"hashKey", "rangeKey"},
		},
		"queue": {Properties: []PreviewField{{Name: "visibilityTimeoutSeconds", After: "30"}}},
		"logs":  {Properties: []PreviewField{{Name: "retentionInDays", Before: "7"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("preview review = %#v, want %#v", got, want)
	}
}
