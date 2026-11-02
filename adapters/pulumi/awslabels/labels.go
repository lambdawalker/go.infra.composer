// Package awslabels applies ownership metadata using actual Pulumi argument
// schemas. It never changes physical names, parents, providers or dependencies.
package awslabels

import (
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/lambdawalker/go.infra.composer/ownership"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type scopeKey struct{}
type scope struct {
	metadata ownership.Metadata
	tracker  *Tracker
}
type pending struct {
	resource                              pulumi.Resource
	kind, name, tagging, descriptionField string
	metadata                              ownership.Metadata
	parent                                pulumi.Input
	parentURN                             pulumi.URNOutput
}

// Tracker retains metadata and safe identifier references, never resource inputs.
type Tracker struct {
	mu      sync.Mutex
	records []pending
	err     error
}

func Install(ctx *pulumi.Context, m ownership.Metadata) (*pulumi.Context, *Tracker, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("ownership context is required")
	}
	if err := m.Validate(); err != nil {
		return nil, nil, err
	}
	if ctx.Value(scopeKey{}) != nil {
		return nil, nil, fmt.Errorf("ownership already installed in this context")
	}
	t := &Tracker{}
	return ctx.WithValue(scopeKey{}, scope{m, t}), t, nil
}
func ForFeature(ctx *pulumi.Context, feature string) (*pulumi.Context, error) {
	s, ok := ctx.Value(scopeKey{}).(scope)
	if !ok {
		return nil, fmt.Errorf("install ownership before selecting a feature")
	}
	s.metadata.Feature = feature
	if err := s.metadata.Validate(); err != nil {
		return nil, err
	}
	return ctx.WithValue(scopeKey{}, s), nil
}

// Option is opt-in for independently embedded native adapters. Application and
// standalone entry points install ownership; old embedding callers remain valid.
func Option(ctx *pulumi.Context) pulumi.ResourceOption {
	s, ok := ctx.Value(scopeKey{}).(scope)
	if !ok {
		return pulumi.Transformations(nil)
	}
	return pulumi.Transformations([]pulumi.ResourceTransformation{func(a *pulumi.ResourceTransformationArgs) *pulumi.ResourceTransformationResult {
		if !strings.HasPrefix(a.Type, "aws:") {
			return nil
		}
		clone, fields := cloneArgs(a.Props)
		p := pending{resource: a.Resource, kind: a.Type, name: a.Name, tagging: "inventory-only", metadata: s.metadata, parent: pulumi.String("")}
		canonical, err := s.metadata.Tags(a.Name)
		if err == nil {
			if f, ok := fields["tags"]; ok {
				tags := pulumi.All(input(f)).ApplyT(func(v []any) (map[string]string, error) {
					out := map[string]string{}
					switch old := v[0].(type) {
					case nil:
					case map[string]string:
						for k, val := range old {
							out[k] = val
						}
					case map[string]any:
						for k, val := range old {
							str, ok := val.(string)
							if !ok {
								return nil, fmt.Errorf("resource tags must contain strings")
							}
							out[k] = str
						}
					default:
						return nil, fmt.Errorf("unsupported provider tag representation")
					}
					for k, val := range canonical {
						if old, ok := out[k]; ok && old != val {
							return nil, fmt.Errorf("reserved ownership tag %s cannot be overridden", k)
						}
						out[k] = val
					}
					return out, nil
				}).(pulumi.StringMapOutput)
				err = set(f, tags)
				p.tagging = "tagged"
			}
		}
		// Explicit allowlist excludes immutable descriptions such as IAM policies.
		if spec, ok := mutableDescriptions[a.Type]; ok && err == nil {
			if f, exists := fields[spec.field]; exists {
				desc := pulumi.All(input(f)).ApplyT(func(v []any) (string, error) {
					purpose := a.Name
					if v[0] != nil {
						switch old := v[0].(type) {
						case string:
							if old != "" {
								purpose = old
							}
						case *string:
							if old != nil && *old != "" {
								purpose = *old
							}
						}
					}
					return s.metadata.Description(purpose, spec.limit)
				}).(pulumi.StringOutput)
				err = set(f, desc)
				p.descriptionField = spec.field
			}
		}
		if field := parentFields[a.Type]; field != "" {
			if f, ok := fields[field]; ok {
				p.parent = input(f)
			}
		}
		options, _ := pulumi.NewResourceOptions(a.Opts...)
		if options.Parent != nil {
			p.parentURN = options.Parent.URN()
		}
		s.tracker.mu.Lock()
		s.tracker.records = append(s.tracker.records, p)
		if err != nil && s.tracker.err == nil {
			s.tracker.err = err
		}
		s.tracker.mu.Unlock()
		return &pulumi.ResourceTransformationResult{Props: clone, Opts: a.Opts}
	}})
}
func input(f reflect.Value) pulumi.Input {
	if !f.IsValid() || ((f.Kind() == reflect.Interface || f.Kind() == reflect.Pointer) && f.IsNil()) {
		return nil
	}
	v, _ := f.Interface().(pulumi.Input)
	return v
}
func set(f reflect.Value, value any) error {
	v := reflect.ValueOf(value)
	if !f.CanSet() || !v.Type().AssignableTo(f.Type()) {
		return fmt.Errorf("provider metadata field has an unsupported type")
	}
	f.Set(v)
	return nil
}
func cloneArgs(props pulumi.Input) (pulumi.Input, map[string]reflect.Value) {
	fields := map[string]reflect.Value{}
	v := reflect.ValueOf(props)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return props, fields
	}
	clone := reflect.New(v.Elem().Type())
	clone.Elem().Set(v.Elem())
	schema := props.ElementType()
	for schema.Kind() == reflect.Pointer {
		schema = schema.Elem()
	}
	for i := 0; i < clone.Elem().NumField(); i++ {
		f := clone.Elem().Type().Field(i)
		key := f.Tag.Get("pulumi")
		// Generated SDK Args expose tags on ElementType, not on input fields.
		if key == "" && schema.Kind() == reflect.Struct {
			if mapped, ok := schema.FieldByName(f.Name); ok {
				key = mapped.Tag.Get("pulumi")
			}
		}
		if key != "" && f.IsExported() {
			fields[key] = clone.Elem().Field(i)
		}
	}
	return clone.Interface().(pulumi.Input), fields
}

// Export snapshots metadata after constructors complete. IDs and URNs stay
// deferred during preview. No inputs, secret values or policy documents appear.
func (t *Tracker) Export(ctx *pulumi.Context, name string) error {
	t.mu.Lock()
	rows := append([]pending(nil), t.records...)
	err := t.err
	t.mu.Unlock()
	if err != nil {
		return err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].kind != rows[j].kind {
			return rows[i].kind < rows[j].kind
		}
		return rows[i].name < rows[j].name
	})
	values := pulumi.Array{}
	for _, r := range rows {
		id := pulumi.String("").ToStringOutput()
		if custom, ok := r.resource.(pulumi.CustomResource); ok {
			id = custom.ID().ToStringOutput()
		}
		parentURN := pulumi.String("").ToStringOutput()
		if r.parentURN.OutputState != nil {
			parentURN = r.parentURN.ToStringOutput()
		}
		tags, _ := r.metadata.Tags(r.name)
		values = append(values, pulumi.All(r.resource.URN(), id, r.parent, parentURN).ApplyT(func(v []any) map[string]any {
			return map[string]any{"type": r.kind, "logicalName": r.name, "urn": fmt.Sprint(v[0]), "id": fmt.Sprint(v[1]), "parentId": stringValue(v[2]), "parentUrn": fmt.Sprint(v[3]), "ownership": tags, "tagging": r.tagging, "taggingReason": taggingReason(r.tagging), "descriptionField": r.descriptionField}
		}))
	}
	ctx.Export(name, values)
	return nil
}
func stringValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

type descriptionSpec struct {
	field string
	limit int
}

var mutableDescriptions = map[string]descriptionSpec{
	"aws:lambda/function:Function": {"description", 256}, "aws:apigatewayv2/api:Api": {"description", 1024}, "aws:apigatewayv2/stage:Stage": {"description", 1024},
	"aws:iam/role:Role": {"description", 1000}, "aws:secretsmanager/secret:Secret": {"description", 2048}, "aws:kms/key:Key": {"description", 8192},
	"aws:cloudwatch/eventRule:EventRule": {"description", 512}, "aws:cloudwatch/metricAlarm:MetricAlarm": {"alarmDescription", 1024},
	"aws:cloudfront/distribution:Distribution": {"comment", 128}, "aws:route53/zone:Zone": {"comment", 256},
}
var parentFields = map[string]string{
	"aws:apigatewayv2/route:Route": "apiId", "aws:apigatewayv2/integration:Integration": "apiId", "aws:apigatewayv2/stage:Stage": "apiId", "aws:apigatewayv2/authorizer:Authorizer": "apiId", "aws:apigatewayv2/apiMapping:ApiMapping": "apiId",
	"aws:cognito/userPoolClient:UserPoolClient": "userPoolId", "aws:cognito/userPoolDomain:UserPoolDomain": "userPoolId",
	"aws:iam/rolePolicy:RolePolicy": "role", "aws:iam/rolePolicyAttachment:RolePolicyAttachment": "role",
	"aws:secretsmanager/secretVersion:SecretVersion": "secretId", "aws:lambda/permission:Permission": "function", "aws:lambda/eventSourceMapping:EventSourceMapping": "functionName",
	"aws:route53/record:Record": "zoneId", "aws:acm/certificateValidation:CertificateValidation": "certificateArn", "aws:cloudwatch/eventTarget:EventTarget": "rule",
	"aws:s3/bucketOwnershipControls:BucketOwnershipControls": "bucket", "aws:cognito/managedLoginBranding:ManagedLoginBranding": "userPoolId",
	"aws:s3/bucketPolicy:BucketPolicy": "bucket", "aws:s3/bucketPublicAccessBlock:BucketPublicAccessBlock": "bucket", "aws:s3/bucketVersioningV2:BucketVersioningV2": "bucket", "aws:s3/bucketServerSideEncryptionConfigurationV2:BucketServerSideEncryptionConfigurationV2": "bucket", "aws:s3/bucketLifecycleConfigurationV2:BucketLifecycleConfigurationV2": "bucket", "aws:s3/bucketObjectv2:BucketObjectv2": "bucket",
	"aws:ses/domainDkim:DomainDkim": "domain",
}

// Ensure supplies standalone defaults only when the application has not installed
// an ownership scope. A non-nil tracker belongs to this caller and must be exported.
func Ensure(ctx *pulumi.Context, environment, origin, feature string, owner ...string) (*pulumi.Context, *Tracker, error) {
	if current, ok := ctx.Value(scopeKey{}).(scope); ok {
		if current.metadata.Environment != environment {
			return nil, nil, fmt.Errorf("ownership environment differs from native deployment")
		}
		if len(owner) > 0 {
			current.metadata.Owner = owner[0]
			if err := current.metadata.Validate(); err != nil {
				return nil, nil, err
			}
			ctx = ctx.WithValue(scopeKey{}, current)
		}
		return ctx, nil, nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return nil, nil, fmt.Errorf("ownership requires a valid application origin")
	}
	repository := ""
	if len(owner) > 0 {
		repository = owner[0]
	}
	return Install(ctx, ownership.Metadata{Owner: repository, Application: ctx.Project(), Environment: environment, Domain: strings.ToLower(u.Hostname()), Feature: feature, Deployment: ctx.Project() + "-" + environment})
}

func taggingReason(mode string) string {
	if mode == "tagged" {
		return "provider exposes a tags input"
	}
	return "provider resource has no supported tags input; ownership tracked in inventory"
}
