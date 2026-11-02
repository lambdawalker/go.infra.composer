package awslabels

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/lambdawalker/go.infra.composer/ownership"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type args struct {
	Tags        pulumi.StringMapInput
	Description pulumi.StringPtrInput
	Name        pulumi.StringInput
	Secret      pulumi.StringInput
}
type resolvedArgs struct {
	Tags        map[string]string `pulumi:"tags"`
	Description string            `pulumi:"description"`
	Name        string            `pulumi:"name"`
	Secret      string            `pulumi:"secret"`
}

func (a *args) ElementType() reflect.Type { return reflect.TypeFor[resolvedArgs]() }

type childArgs struct {
	ApiId pulumi.StringInput `pulumi:"apiId"`
}

func (a *childArgs) ElementType() reflect.Type {
	return reflect.TypeFor[struct {
		ApiId string `pulumi:"apiId"`
	}]()
}

type monitor struct {
	mu    sync.Mutex
	items map[string]pulumi.MockResourceArgs
}

func (m *monitor) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) { return a.Args, nil }
func (m *monitor) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[a.Name] = a
	return a.Name + "-id", a.Inputs, nil
}
func meta() ownership.Metadata {
	return ownership.Metadata{Application: "attestra", Environment: "dev", Domain: "attestrabond.com", Feature: "application", Deployment: "attestra-dev"}
}
func TestLabelsPreserveArgsOptionsAndInventory(t *testing.T) {
	m := &monitor{items: map[string]pulumi.MockResourceArgs{}}
	original := &args{Tags: pulumi.StringMap{"team": pulumi.String("auth")}, Description: pulumi.String("Starts verification"), Name: pulumi.String("unchanged-physical-name"), Secret: pulumi.ToSecret("never-export-this").(pulumi.StringOutput)}
	checked := false
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		ctx, tracker, err := Install(ctx, meta())
		if err != nil {
			return err
		}
		scoped, err := ForFeature(ctx, "verifyemail")
		if err != nil {
			return err
		}
		var fn pulumi.CustomResourceState
		if err := scoped.RegisterResource("aws:lambda/function:Function", "signup", original, &fn, pulumi.Protect(true), Option(scoped)); err != nil {
			return err
		}
		var child pulumi.CustomResourceState
		if err := scoped.RegisterResource("aws:apigatewayv2/route:Route", "signup-route", &childArgs{ApiId: fn.ID()}, &child, Option(scoped)); err != nil {
			return err
		}
		if err := tracker.Export(ctx, "inventory"); err != nil {
			return err
		}
		pulumi.All(ctx.GetCurrentExportMap()["inventory"]).ApplyT(func(v []any) error {
			rows := v[0].([]any)
			if len(rows) != 2 {
				t.Errorf("incomplete inventory: %v", rows)
			}
			found := false
			for _, row := range rows {
				record := row.(map[string]any)
				if strings.Contains(record["type"].(string), "route") {
					found = true
					if record["tagging"] != "inventory-only" || record["parentId"] != "signup-id" {
						t.Errorf("child ownership lost: %v", record)
					}
				}
				for _, val := range record {
					if strings.Contains(strings.TrimSpace(reflect.ValueOf(val).String()), "never-export-this") {
						t.Error("secret leaked")
					}
				}
			}
			checked = found
			return nil
		})
		return nil
	}, pulumi.WithMocks("test", "dev", m))
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("inventory not resolved")
	}
	fn := m.items["signup"]
	tags := fn.Inputs["tags"].ObjectValue()
	if tags["ic:feature"].StringValue() != "verifyemail" || tags["team"].StringValue() != "auth" {
		t.Fatal(tags)
	}
	if fn.Inputs["name"].StringValue() != "unchanged-physical-name" || !fn.RegisterRPC.GetProtect() {
		t.Fatal("identity or options changed")
	}
	if fn.Inputs["description"].StringValue() != "[dev]/[attestrabond.com] attestra/verifyemail - Starts verification" {
		t.Fatal(fn.Inputs)
	}
	if original.Tags.(pulumi.StringMap)["ic:feature"] != nil {
		t.Fatal("caller args mutated")
	}
}
func TestInvalidMetadataAndReservedOverrideFail(t *testing.T) {
	m := &monitor{items: map[string]pulumi.MockResourceArgs{}}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		bad := meta()
		bad.Domain = "https://bad"
		_, _, e := Install(ctx, bad)
		return e
	}, pulumi.WithMocks("test", "dev", m))
	if err == nil || len(m.items) != 0 {
		t.Fatal("invalid metadata registered resources", err)
	}
	err = pulumi.RunErr(func(ctx *pulumi.Context) error {
		ctx, _, e := Install(ctx, meta())
		if e != nil {
			return e
		}
		var r pulumi.CustomResourceState
		return ctx.RegisterResource("aws:lambda/function:Function", "wrong-owner", &args{Tags: pulumi.StringMap{"ic:application": pulumi.String("other")}}, &r, Option(ctx))
	}, pulumi.WithMocks("test", "dev", m))
	if err == nil {
		t.Fatal("ownership override accepted")
	}
}
func TestImmutablePolicyDescriptionRemainsUnchanged(t *testing.T) {
	m := &monitor{items: map[string]pulumi.MockResourceArgs{}}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		ctx, _, e := Install(ctx, meta())
		if e != nil {
			return e
		}
		var r pulumi.CustomResourceState
		return ctx.RegisterResource("aws:iam/policy:Policy", "policy", &args{Description: pulumi.String("Do not replace this policy")}, &r, Option(ctx))
	}, pulumi.WithMocks("test", "dev", m))
	if err != nil {
		t.Fatal(err)
	}
	if m.items["policy"].Inputs["description"].StringValue() != "Do not replace this policy" {
		t.Fatal("immutable policy description modified")
	}
}

type parentArgs struct {
	Bucket     pulumi.StringInput
	UserPoolId pulumi.StringInput
}

func (a *parentArgs) ElementType() reflect.Type {
	return reflect.TypeFor[struct {
		Bucket     string `pulumi:"bucket"`
		UserPoolId string `pulumi:"userPoolId"`
	}]()
}
func TestInventoryLinksS3ControlsAndCognitoBrandingParents(t *testing.T) {
	m := &monitor{items: map[string]pulumi.MockResourceArgs{}}
	checked := false
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		ctx, labels, err := Install(ctx, meta())
		if err != nil {
			return err
		}
		for name, kind := range map[string]string{"controls": "aws:s3/bucketOwnershipControls:BucketOwnershipControls", "branding": "aws:cognito/managedLoginBranding:ManagedLoginBranding"} {
			var r pulumi.CustomResourceState
			if err := ctx.RegisterResource(kind, name, &parentArgs{Bucket: pulumi.String("bucket-id"), UserPoolId: pulumi.String("pool-id")}, &r, Option(ctx)); err != nil {
				return err
			}
		}
		if err := labels.Export(ctx, "inventory"); err != nil {
			return err
		}
		pulumi.All(ctx.GetCurrentExportMap()["inventory"]).ApplyT(func(v []any) error {
			for _, row := range v[0].([]any) {
				r := row.(map[string]any)
				want := "bucket-id"
				if r["logicalName"] == "branding" {
					want = "pool-id"
				}
				if r["parentId"] != want {
					t.Errorf("missing child parent: %v", r)
				}
			}
			checked = true
			return nil
		})
		return nil
	}, pulumi.WithMocks("test", "dev", m))
	if err != nil || !checked {
		t.Fatal(err, checked)
	}
}
