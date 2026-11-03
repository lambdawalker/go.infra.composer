package pulumicli

import (
	"reflect"
	"strings"
	"testing"
	"time"

	r "github.com/lambdawalker/go.infra.composer/reporting"
)

func TestStateRecordedConfigurationAllowlist(t *testing.T) {
	at := time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		typ, outputs string
		want         map[string]string
	}{
		{"aws:lambda/function:Function", `{"memorySize":512,"timeout":3e1,"reservedConcurrentExecutions":-1}`, map[string]string{"memorySize": "512", "timeout": "30", "reservedConcurrentExecutions": "-1"}},
		{"aws:sqs/queue:Queue", `{"visibilityTimeoutSeconds":30,"messageRetentionSeconds":345600,"delaySeconds":0,"receiveWaitTimeSeconds":20,"maxMessageSize":262144}`, map[string]string{"visibilityTimeoutSeconds": "30", "messageRetentionSeconds": "345600", "delaySeconds": "0", "receiveWaitTimeSeconds": "20", "maxMessageSize": "262144"}},
		{"aws:cloudwatch/logGroup:LogGroup", `{"retentionInDays":0}`, map[string]string{"retentionInDays": "0"}},
		{"aws:dynamodb/table:Table", `{"billingMode":"PAY_PER_REQUEST","readCapacity":0,"writeCapacity":5,"deletionProtectionEnabled":false}`, map[string]string{"billingMode": "PAY_PER_REQUEST", "readCapacity": "0", "writeCapacity": "5", "deletionProtectionEnabled": "false"}},
		{"aws:dynamodb/table:Table", `{"billingMode":"PROVISIONED","deletionProtectionEnabled":true}`, map[string]string{"billingMode": "PROVISIONED", "deletionProtectionEnabled": "true"}},
		{"aws:lambda/function:Function", `{}`, nil},
		{"aws:cloudwatch/logGroup:LogGroup", `{}`, nil},
		{"aws:dynamodb/table:Table", `{}`, nil},
		{"aws:lambda/function:Function", `{"memorySize":{"secret":"secret-marker"},"timeout":"30","reservedConcurrentExecutions":[1],"environment":{"TOKEN":"secret-marker"},"policy":"secret-marker","url":"secret-marker"}`, nil},
		{"aws:dynamodb/table:Table", `{"billingMode":"secret-marker","readCapacity":1.5,"writeCapacity":null,"deletionProtectionEnabled":"true"}`, nil},
		{"aws:s3/bucket:Bucket", `{"memorySize":512,"retentionInDays":7,"lifecycleRules":[{"secret":"secret-marker"}]}`, nil},
	} {
		t.Run(tc.typ+tc.outputs, func(t *testing.T) {
			input := `{"deployment":{"resources":[{"urn":"a","id":"id","type":"` + tc.typ + `","outputs":` + tc.outputs + `,"inputs":{"memorySize":1024,"timeout":60,"retentionInDays":7,"billingMode":"PROVISIONED"}}]}}`
			as, err := ReadState(strings.NewReader(input), at)
			if err != nil || len(as) != 1 {
				t.Fatalf("%#v %v", as, err)
			}
			c := as[0].Configuration
			if tc.want == nil {
				if c != nil {
					t.Fatalf("unsafe configuration: %#v", c)
				}
			} else if c == nil || c.Source != "pulumi-state" || c.ObservedAt != at || !reflect.DeepEqual(c.Values, tc.want) {
				t.Fatalf("configuration: %#v, want %v", c, tc.want)
			}
		})
	}
}

func TestRecordedConfigurationIndependentOfRefreshAndPreview(t *testing.T) {
	at := time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)
	as, err := ReadState(strings.NewReader(`{"deployment":{"resources":[{"urn":"a","id":"id","type":"aws:lambda/function:Function","outputs":{"memorySize":512}}]}}`), at)
	if err != nil {
		t.Fatal(err)
	}
	preview := `{"resourcePreEvent":{"metadata":{"urn":"a","type":"aws:lambda/function:Function","op":"update","new":{"outputs":{"memorySize":1024}}}}}
{"resourcePreEvent":{"metadata":{"urn":"new","type":"aws:lambda/function:Function","op":"create","new":{"outputs":{"memorySize":1024}}}}}`
	got, err := ApplyPreview(as, strings.NewReader(preview))
	if err != nil || got[0].Configuration == nil || got[0].Configuration.Values["memorySize"] != "512" || got[0].Configuration.ObservedAt != at || got[1].Configuration != nil {
		t.Fatalf("preview invented settings: %#v %v", got, err)
	}
	for _, op := range []string{"same", "delete", "refresh"} {
		event := `{"resourceOutputsEvent":{"metadata":{"urn":"a","op":"` + op + `","old":{"id":"id"}`
		if op == "same" {
			event += `,"new":{"id":"id","outputs":{"memorySize":2048}}`
		}
		event += `}}}`
		fresh, err := ReadRefresh(strings.NewReader(event), got, at.Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		c := r.Normalize(r.Report{Artifacts: fresh}).Artifacts[0].Configuration
		if c == nil || c.ObservedAt != at || c.Values["memorySize"] != "512" || fresh[0].Observation.ObservedAt != at.Add(time.Hour) {
			t.Fatalf("configuration freshness changed: %#v", fresh)
		}
	}
}
