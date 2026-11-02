# AWS resource ownership labels

The package uses the Pulumi Go SDK without importing AWS provider SDKs into the core. Supply a validated `ownership.Metadata` scope once, select a feature scope at composition boundaries, and pass `awslabels.Option(ctx)` to each native AWS constructor.

```go
ctx, inventory, err := awslabels.Install(ctx, ownership.Metadata{
    Application: "attestra", Environment: "dev", Domain: "attestrabond.com",
    Feature: "application", Deployment: "attestra-dev",
})
if err != nil { return err }
featureCtx, err := awslabels.ForFeature(ctx, "verifyemail")
if err != nil { return err }
// lambda.NewFunction(featureCtx, "signup", args, awslabels.Option(featureCtx))
// Other AWS constructors forward the same option.
return inventory.Export(ctx, "resourceInventory")
```

`Ensure` is for standalone native adapters: use an existing app scope or create a validated project/environment/origin scope. A non-nil returned tracker belongs to that caller and must be exported after construction. `Option` remains a no-op for old embedding callers that did not install a scope; framework entry points and maintained native adapters always install/ensure one.

Tags are merged into the provider's actual tag-map input, discovered from the argument ElementType schema. Unrelated tags survive; changing reserved ownership values fails. Descriptions use an explicit mutable-property allowlist with service length limits. IAM managed-policy descriptions are excluded to avoid replacements. Names, parents, providers, protection and dependency options are preserved.

The inventory includes type, logical name, URN, provider ID, ownership labels, parent ID/URN and whether tags were supported. It exports no constructor arguments, policy bodies, environment maps or secret values. Missing SDK tag support (including legacy SES domain resources) is recorded as inventory-only. Cross-region providers receive labels through the same resource option. Context-scoped metadata avoids a process-global current-feature variable.

This is a resource-option API for typed provider argument structs. Untyped `pulumi.Map` inputs have no provider schema and are reported inventory-only. Provider resources, data-source invokes and resources created outside these constructors are not claimed by this tracker. Inspect the exported coverage and keep native mock tests asserting complete registration coverage.
