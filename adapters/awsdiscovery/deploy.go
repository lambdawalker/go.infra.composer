// Package awsdiscovery provisions a shared environment index in an AWS Pulumi stack.
package awsdiscovery

import (
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"

	"github.com/lambdawalker/go.infra.composer/discovery"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/acm"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/apigatewayv2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/route53"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Settings struct {
	Domain, DNSProvider, DNSZoneID string
	Code                           pulumi.ArchiveInput
}

type Result struct {
	URL          pulumi.StringOutput
	ExecutionARN pulumi.StringOutput
}

var (
	dnsLabel       = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	route53Zone    = regexp.MustCompile(`^Z[A-Z0-9]+$`)
	cloudflareZone = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

func (s Settings) validate() error {
	if len(s.Domain) > 253 || !strings.Contains(s.Domain, ".") || net.ParseIP(s.Domain) != nil {
		return fmt.Errorf("discovery requires a DNS hostname")
	}
	for _, label := range strings.Split(s.Domain, ".") {
		if !dnsLabel.MatchString(label) {
			return fmt.Errorf("discovery requires a lowercase DNS hostname")
		}
	}
	switch s.DNSProvider {
	case "route53":
		if !route53Zone.MatchString(s.DNSZoneID) {
			return fmt.Errorf("discovery requires a Route 53 public zone ID")
		}
	case "cloudflare":
		if !cloudflareZone.MatchString(s.DNSZoneID) {
			return fmt.Errorf("discovery requires a Cloudflare zone ID")
		}
	default:
		return fmt.Errorf("discovery DNS provider must be route53 or cloudflare")
	}
	if s.Code == nil {
		return fmt.Errorf("discovery Lambda archive is required")
	}
	return nil
}

// Deploy creates one index for the whole application. Environment stacks
// publish through the IAM-authorized route after applying and becoming ready.
func Deploy(ctx *pulumi.Context, name string, s Settings, opts ...pulumi.ResourceOption) (Result, error) {
	if err := s.validate(); err != nil {
		return Result{}, err
	}
	table, err := dynamodb.NewTable(ctx, name+"-registry", &dynamodb.TableArgs{
		BillingMode: pulumi.String("PAY_PER_REQUEST"), HashKey: pulumi.String("id"),
		Attributes:           dynamodb.TableAttributeArray{&dynamodb.TableAttributeArgs{Name: pulumi.String("id"), Type: pulumi.String("S")}},
		PointInTimeRecovery:  &dynamodb.TablePointInTimeRecoveryArgs{Enabled: pulumi.Bool(true)},
		ServerSideEncryption: &dynamodb.TableServerSideEncryptionArgs{Enabled: pulumi.Bool(true)},
	}, append(slices.Clone(opts), pulumi.Protect(true))...)
	if err != nil {
		return Result{}, err
	}
	role, err := iam.NewRole(ctx, name+"-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`),
	}, opts...)
	if err != nil {
		return Result{}, err
	}
	logs, err := cloudwatch.NewLogGroup(ctx, name+"-logs", &cloudwatch.LogGroupArgs{RetentionInDays: pulumi.Int(14)}, opts...)
	if err != nil {
		return Result{}, err
	}
	policy, err := iam.NewRolePolicy(ctx, name+"-policy", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["dynamodb:Scan","dynamodb:UpdateItem"],"Resource":%q},{"Effect":"Allow","Action":["logs:CreateLogStream","logs:PutLogEvents"],"Resource":%q}]}`, table.Arn, pulumi.Sprintf("%s:*", logs.Arn)),
	}, opts...)
	if err != nil {
		return Result{}, err
	}
	fn, err := lambda.NewFunction(ctx, name+"-handler", &lambda.FunctionArgs{
		Code: s.Code, Role: role.Arn, Handler: pulumi.String("bootstrap"), Runtime: pulumi.String("provided.al2023"),
		Architectures: pulumi.StringArray{pulumi.String("arm64")}, MemorySize: pulumi.Int(128), Timeout: pulumi.Int(10),
		LoggingConfig: &lambda.FunctionLoggingConfigArgs{LogFormat: pulumi.String("Text"), LogGroup: logs.Name},
		Environment:   &lambda.FunctionEnvironmentArgs{Variables: pulumi.StringMap{"DISCOVERY_TABLE": table.Name}},
	}, append(slices.Clone(opts), pulumi.DependsOn([]pulumi.Resource{policy}))...)
	if err != nil {
		return Result{}, err
	}
	api, err := apigatewayv2.NewApi(ctx, name+"-api", &apigatewayv2.ApiArgs{
		ProtocolType: pulumi.String("HTTP"), DisableExecuteApiEndpoint: pulumi.Bool(true),
	}, opts...)
	if err != nil {
		return Result{}, err
	}
	integration, err := apigatewayv2.NewIntegration(ctx, name+"-integration", &apigatewayv2.IntegrationArgs{
		ApiId: api.ID(), IntegrationType: pulumi.String("AWS_PROXY"), IntegrationUri: fn.InvokeArn,
		PayloadFormatVersion: pulumi.String("2.0"),
	}, opts...)
	if err != nil {
		return Result{}, err
	}
	for _, route := range []struct{ name, method, path, auth string }{
		{"read", "GET", discovery.Path, "NONE"},
		{"write", "POST", discovery.Path + "/{environment}/changes", "AWS_IAM"},
	} {
		_, err = apigatewayv2.NewRoute(ctx, name+"-"+route.name, &apigatewayv2.RouteArgs{
			ApiId: api.ID(), RouteKey: pulumi.String(route.method + " " + route.path),
			AuthorizationType: pulumi.String(route.auth), Target: pulumi.Sprintf("integrations/%s", integration.ID()),
		}, opts...)
		if err != nil {
			return Result{}, err
		}
		path := strings.ReplaceAll(route.path, "{environment}", "*")
		_, err = lambda.NewPermission(ctx, name+"-"+route.name+"-invoke", &lambda.PermissionArgs{
			Action: pulumi.String("lambda:InvokeFunction"), Function: fn.Name,
			Principal: pulumi.String("apigateway.amazonaws.com"),
			SourceArn: pulumi.Sprintf("%s/$default/%s%s", api.ExecutionArn, route.method, path),
		}, opts...)
		if err != nil {
			return Result{}, err
		}
	}
	stage, err := apigatewayv2.NewStage(ctx, name+"-stage", &apigatewayv2.StageArgs{
		ApiId: api.ID(), Name: pulumi.String("$default"), AutoDeploy: pulumi.Bool(true),
		DefaultRouteSettings: &apigatewayv2.StageDefaultRouteSettingsArgs{
			ThrottlingBurstLimit: pulumi.Int(20), ThrottlingRateLimit: pulumi.Float64(10),
		},
	}, opts...)
	if err != nil {
		return Result{}, err
	}
	hostname, err := provisionDomain(ctx, name, s, api, stage, opts)
	if err != nil {
		return Result{}, err
	}
	return Result{URL: pulumi.Sprintf("https://%s%s", hostname, discovery.Path), ExecutionARN: api.ExecutionArn}, nil
}

func provisionDomain(ctx *pulumi.Context, name string, s Settings, api *apigatewayv2.Api, stage *apigatewayv2.Stage, opts []pulumi.ResourceOption) (pulumi.StringOutput, error) {
	cert, err := acm.NewCertificate(ctx, name+"-certificate", &acm.CertificateArgs{
		DomainName: pulumi.String(s.Domain), ValidationMethod: pulumi.String("DNS"),
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	challenge := cert.DomainValidationOptions.ApplyT(func(options []acm.CertificateDomainValidationOption) (acm.CertificateDomainValidationOption, error) {
		for _, option := range options {
			if option.DomainName != nil && *option.DomainName == s.Domain && option.ResourceRecordType != nil &&
				*option.ResourceRecordType == "CNAME" && option.ResourceRecordName != nil && option.ResourceRecordValue != nil {
				return option, nil
			}
		}
		return acm.CertificateDomainValidationOption{}, fmt.Errorf("missing ACM validation CNAME for the index")
	}).(acm.CertificateDomainValidationOptionOutput)
	fqdn, err := cname(ctx, name+"-certificate-dns", s, challenge.ResourceRecordName().Elem(), challenge.ResourceRecordValue().Elem(), opts)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	validation, err := acm.NewCertificateValidation(ctx, name+"-certificate-validated", &acm.CertificateValidationArgs{
		CertificateArn: cert.Arn, ValidationRecordFqdns: pulumi.StringArray{fqdn},
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	custom, err := apigatewayv2.NewDomainName(ctx, name+"-domain", &apigatewayv2.DomainNameArgs{
		DomainName: pulumi.String(s.Domain), DomainNameConfiguration: &apigatewayv2.DomainNameDomainNameConfigurationArgs{
			CertificateArn: validation.CertificateArn, EndpointType: pulumi.String("REGIONAL"), SecurityPolicy: pulumi.String("TLS_1_2"),
		},
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	mapping, err := apigatewayv2.NewApiMapping(ctx, name+"-mapping", &apigatewayv2.ApiMappingArgs{
		ApiId: api.ID(), DomainName: custom.ID(), Stage: stage.Name,
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	return cname(ctx, name+"-hostname", s, pulumi.String(s.Domain), custom.DomainNameConfiguration.TargetDomainName().Elem(),
		append(slices.Clone(opts), pulumi.DependsOn([]pulumi.Resource{mapping})))
}

func cname(ctx *pulumi.Context, name string, s Settings, host, target pulumi.StringInput, opts []pulumi.ResourceOption) (pulumi.StringOutput, error) {
	if s.DNSProvider == "cloudflare" {
		trim := func(input pulumi.StringInput) pulumi.StringOutput {
			return input.ToStringOutput().ApplyT(func(value string) string {
				return strings.TrimSuffix(value, ".")
			}).(pulumi.StringOutput)
		}
		var record struct {
			pulumi.CustomResourceState
			Name pulumi.StringOutput `pulumi:"name"`
		}
		err := ctx.RegisterResource("cloudflare:index/dnsRecord:DnsRecord", name, pulumi.Map{
			"zoneId": pulumi.String(s.DNSZoneID), "name": trim(host), "content": trim(target),
			"type": pulumi.String("CNAME"), "ttl": pulumi.Float64(300), "proxied": pulumi.Bool(false),
		}, &record, append(slices.Clone(opts), pulumi.Version("6.20.0"))...)
		return record.Name, err
	}
	record, err := route53.NewRecord(ctx, name, &route53.RecordArgs{
		ZoneId: pulumi.String(s.DNSZoneID), Name: host, Type: pulumi.String("CNAME"),
		Ttl: pulumi.Int(300), Records: pulumi.StringArray{target},
	}, opts...)
	if err != nil {
		return pulumi.StringOutput{}, err
	}
	return record.Fqdn.ApplyT(func(value string) string { return strings.TrimSuffix(value, ".") }).(pulumi.StringOutput), nil
}
