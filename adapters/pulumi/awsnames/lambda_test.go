package awsnames

import (
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestLambdaFunctionName(t *testing.T) {
	cases := []struct {
		label, logical string
		explicit       pulumi.StringInput
		wantError      bool
	}{
		{label: "default auto-name", logical: "testhost-app-lambda"},
		{label: "dotted auto-name base", logical: "testhost.app-lambda", wantError: true},
		{label: "long auto-name base", logical: strings.Repeat("a", 65), wantError: true},
		{label: "explicit name overrides dotted base", logical: "testhost.app-lambda", explicit: pulumi.String("Test_Host-1")},
		{label: "maximum explicit length", logical: "function", explicit: pulumi.String(strings.Repeat("a", 64))},
		{label: "long explicit name", logical: "function", explicit: pulumi.String(strings.Repeat("a", 65)), wantError: true},
		{label: "empty explicit name", logical: "function", explicit: pulumi.String(""), wantError: true},
		{label: "space", logical: "function", explicit: pulumi.String("my function"), wantError: true},
		{label: "non-ASCII", logical: "function", explicit: pulumi.String("función"), wantError: true},
		{label: "ARN", logical: "function", explicit: pulumi.String("arn:aws:lambda:us-east-1:123456789012:function:test"), wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got, err := LambdaFunctionName(tc.logical, tc.explicit)
			if (err != nil) != tc.wantError {
				t.Fatalf("got %v, want error: %t", err, tc.wantError)
			}
			if err == nil && got != tc.explicit {
				t.Fatalf("name changed: got %v, want %v", got, tc.explicit)
			}
		})
	}
}

type nameMocks struct {
	observedName string
	dependency   bool
}

func (m *nameMocks) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) { return a.Args, nil }

func (m *nameMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if a.Name == "fixture-function" {
		m.observedName = a.Inputs["name"].StringValue()
		for _, urn := range a.RegisterRPC.GetPropertyDependencies()["name"].GetUrns() {
			m.dependency = m.dependency || strings.HasSuffix(urn, "::name-source")
		}
	}
	return a.Name + "-id", a.Inputs, nil
}

func TestLambdaFunctionNameChecksComputedInput(t *testing.T) {
	for _, value := range []string{"worker-1", "worker.invalid"} {
		t.Run(value, func(t *testing.T) {
			m := &nameMocks{}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				var source struct {
					pulumi.CustomResourceState
					Value pulumi.StringOutput `pulumi:"value"`
				}
				if err := ctx.RegisterResource("fixture:index:Source", "name-source", pulumi.Map{"value": pulumi.String(value)}, &source); err != nil {
					return err
				}
				checked, err := LambdaFunctionName("fixture-function", source.Value)
				if err != nil {
					return err
				}
				var function pulumi.CustomResourceState
				return ctx.RegisterResource("aws:lambda/function:Function", "fixture-function", pulumi.Map{"name": checked}, &function)
			}, pulumi.WithMocks("fixture", "test", m))
			if strings.Contains(value, ".") {
				if err == nil || !strings.Contains(err.Error(), "the function name must contain") || m.observedName != "" {
					t.Fatalf("invalid name reached registration or incorrect error: %v, name %q", err, m.observedName)
				}
				return
			}
			if err != nil || m.observedName != value || !m.dependency {
				t.Fatalf("registration lost the value or dependency: %v, %+v", err, m)
			}
		})
	}
}
