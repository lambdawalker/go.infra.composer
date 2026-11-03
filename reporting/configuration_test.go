package reporting

import (
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConfigurationNormalizeCopiesAndSanitizes(t *testing.T) {
	at := time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)
	c := &Configuration{Source: "pulumi-state", ObservedAt: at, Values: map[string]string{
		"memorySize": "512", "timeout": "3e1", "reservedConcurrentExecutions": "-1",
		"environment": "secret-marker", "<script>": "secret-marker",
	}}
	a := Artifact{Key: "a", Type: "aws:lambda/function:Function", Configuration: c}
	got := Normalize(Report{Artifacts: []Artifact{a}}).Artifacts[0]
	want := map[string]string{"memorySize": "512", "timeout": "30", "reservedConcurrentExecutions": "-1"}
	if got.Configuration == c || !reflect.DeepEqual(got.Configuration.Values, want) || got.Configuration.ObservedAt != at {
		t.Fatalf("unsafe configuration: %#v", got.Configuration)
	}
	c.Values["memorySize"] = "1024"
	c.Source = "changed"
	if got.Configuration.Values["memorySize"] != "512" || got.Configuration.Source != "pulumi-state" {
		t.Fatal("configuration aliases caller")
	}
	for _, value := range []string{"NaN", "Inf", "1e309", "1.5", "9007199254740992.1", `"512"`, "null", "true", `{"secret":"512"}`, "[512]", "secret-marker"} {
		a.Configuration = &Configuration{Source: "pulumi-state", ObservedAt: at, Values: map[string]string{"memorySize": value}}
		if Normalize(Report{Artifacts: []Artifact{a}}).Artifacts[0].Configuration != nil {
			t.Errorf("accepted invalid integer %q", value)
		}
	}
	for _, invalid := range []Artifact{
		{Key: "a", Type: "aws:s3/bucket:Bucket", Configuration: got.Configuration},
		{Key: "a", Type: "custom:lambda/function:Function", Configuration: got.Configuration},
		{Key: "a", Type: a.Type, Configuration: &Configuration{Source: "preview", ObservedAt: at, Values: want}},
		{Key: "a", Type: a.Type, Configuration: &Configuration{Source: "pulumi-state", Values: want}},
	} {
		if Normalize(Report{Artifacts: []Artifact{invalid}}).Artifacts[0].Configuration != nil {
			t.Fatalf("accepted unsupported configuration: %#v", invalid)
		}
	}
}

func TestConfigurationRenderAndOldReportCompatibility(t *testing.T) {
	at := time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)
	r := Report{Artifacts: []Artifact{{Key: "a", Name: "<script>|[name]", Type: "aws:lambda/function:Function", Configuration: &Configuration{
		Source: "pulumi-state", ObservedAt: at, Values: map[string]string{"timeout": "30", "memorySize": "512", "<script>": "secret-marker", "environment": "secret-marker"},
	}}}}
	for _, render := range []func(io.Writer, Report) error{WriteHTML, WriteMarkdown} {
		var b strings.Builder
		if err := render(&b, r); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Recorded configuration", "Pulumi state outputs", "Configuration state read at", "pulumi-state", "2026-11-03T01:00:00Z", "memorySize", "512", "timeout", "30", "&lt;script&gt;"} {
			if !strings.Contains(b.String(), want) {
				t.Errorf("missing %q", want)
			}
		}
		if strings.Contains(b.String(), "secret-marker") || strings.Contains(b.String(), "<script>") || strings.Index(b.String(), "memorySize") > strings.Index(b.String(), "timeout") {
			t.Fatal("unsafe or nondeterministic rendering")
		}
	}
	var current strings.Builder
	if err := WriteJSON(&current, r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"configuration": {`, `"source": "pulumi-state"`, `"observedAt": "2026-11-03T01:00:00Z"`, `"memorySize": "512"`} {
		if !strings.Contains(current.String(), want) {
			t.Errorf("missing JSON configuration %q", want)
		}
	}
	if strings.Contains(current.String(), "secret-marker") {
		t.Fatal("unsafe JSON configuration")
	}
	var old Report
	if err := json.Unmarshal([]byte(`{"schemaVersion":1,"scope":{},"metadata":{},"artifacts":[{"key":"old","observation":{"status":"unknown"}}]}`), &old); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := WriteJSON(&b, old); err != nil || strings.Contains(b.String(), `"configuration"`) || !strings.Contains(b.String(), `"schemaVersion": 1`) {
		t.Fatalf("old report incompatible: %s %v", b.String(), err)
	}
}
