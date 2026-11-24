package awsdiscovery

import (
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type mocks struct {
	mu     sync.Mutex
	routes map[string]string
}

func (m *mocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) { return args.Args, nil }

func (m *mocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	state := args.Inputs.Copy()
	state["arn"] = resource.NewStringProperty("arn:aws:mock:us-east-2:123456789012:" + args.Name)
	switch args.TypeToken {
	case "aws:acm/certificate:Certificate":
		host := args.Inputs["domainName"].StringValue()
		state["domainValidationOptions"] = resource.NewArrayProperty([]resource.PropertyValue{
			resource.NewObjectProperty(resource.PropertyMap{
				"domainName": resource.NewStringProperty(host), "resourceRecordName": resource.NewStringProperty("_proof." + host),
				"resourceRecordType": resource.NewStringProperty("CNAME"), "resourceRecordValue": resource.NewStringProperty("_token.acm-validations.aws."),
			}),
		})
	case "aws:apigatewayv2/api:Api":
		state["executionArn"] = resource.NewStringProperty("arn:aws:execute-api:us-east-2:123456789012:fixture")
	case "aws:apigatewayv2/domainName:DomainName":
		state["domainNameConfiguration"] = resource.NewObjectProperty(resource.PropertyMap{
			"targetDomainName": resource.NewStringProperty("index.execute-api.us-east-2.amazonaws.com"),
		})
	case "aws:route53/record:Record":
		state["fqdn"] = resource.NewStringProperty(args.Inputs["name"].StringValue() + ".")
	case "aws:apigatewayv2/route:Route":
		m.mu.Lock()
		m.routes[args.Inputs["routeKey"].StringValue()] = args.Inputs["authorizationType"].StringValue()
		m.mu.Unlock()
	}
	return args.Name + "-id", state, nil
}

func TestSharedIndexRoutesAndURL(t *testing.T) {
	m := &mocks{routes: make(map[string]string)}
	const host = "index.components.example.test"
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		result, err := Deploy(ctx, "fixture", Settings{
			Domain: host, DNSProvider: "route53", DNSZoneID: "Z123",
			Code: pulumi.NewAssetArchive(map[string]interface{}{"bootstrap": pulumi.NewStringAsset("fixture")}),
		})
		if err != nil {
			return err
		}
		ctx.Export("index", result.URL.ApplyT(func(index string) string {
			if index != "https://"+host+"/v1/environments" {
				t.Errorf("unexpected directory URL %q", index)
			}
			return index
		}))
		return nil
	}, pulumi.WithMocks("fixture", "shared", m))
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.routes) != 2 || m.routes["GET /v1/environments"] != "NONE" ||
		m.routes["POST /v1/environments/{environment}/changes"] != "AWS_IAM" {
		t.Fatalf("unexpected authorization configuration: %v", m.routes)
	}
}
