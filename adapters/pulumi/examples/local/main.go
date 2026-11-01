// This program registers only local components. It provisions no cloud resources.
package main

import (
	"fmt"

	composer "github.com/lambdawalker/go.infra.composer"
	adapter "github.com/lambdawalker/go.infra.composer/adapters/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type messageResource struct{ pulumi.ResourceState }

func messageFactory(ctx *pulumi.Context, name string, inputs pulumi.Map, options ...pulumi.ResourceOption) (adapter.Registered, error) {
	component := &messageResource{}
	if err := ctx.RegisterComponentResource("composer:local:Message", name, component, options...); err != nil {
		return adapter.Registered{}, err
	}
	message := pulumi.ToOutput(inputs["message"]).ApplyT(func(value any) (string, error) {
		text, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("message must be a string")
		}
		return text, nil
	}).(pulumi.StringOutput)
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"message": message}); err != nil {
		return adapter.Registered{}, err
	}
	return adapter.Registered{Resource: component, Outputs: map[string]pulumi.Output{"message": message}}, nil
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		feature := &composer.Feature{ID: "Local", Resources: []composer.Resource{
			{ID: "greeting", Kind: "local:message", Inputs: map[string]composer.Value{"message": composer.Literal("Hello from a composer plan")}, Outputs: map[string]composer.Field{"message": {Type: composer.String}}},
			{ID: "echo", Kind: "local:message", Inputs: map[string]composer.Value{"message": composer.ResourceRef("Local.greeting", "message")}, Outputs: map[string]composer.Field{"message": {Type: composer.String}}},
		}, Offerings: []composer.Offering{{ID: "greeting", Contract: composer.Contract{ID: "local.message", Major: 1, Fields: map[string]composer.Field{"message": {Type: composer.String}}}, Values: map[string]composer.Value{"message": composer.ResourceRef("Local.echo", "message")}}}}
		plan, err := composer.NewApplication("composer-local", feature).Plan(composer.Environment{Name: ctx.Stack()})
		if err != nil {
			return err
		}
		_, err = adapter.Execute(ctx, plan, adapter.Registry{"local:message": messageFactory})
		return err
	})
}
