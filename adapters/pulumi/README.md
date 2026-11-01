# Native Pulumi adapter

This optional nested Go module keeps the Pulumi dependency outside the composer core. It pins the official Pulumi Go SDK v3.273.0 and requires Go 1.27.3 or newer.

Call `pulumiadapter.Execute(ctx, plan, registry)` inside `pulumi.Run`. `registry` is a bounded map from composer kinds to trusted Go factories. A factory calls a provider SDK constructor (or `ctx.RegisterResource`), forwards the supplied resource options, and returns its resource and named native outputs. It must expose every declared output. No external constructor names, executable arguments, or serialized plans are accepted.

The adapter validates all kinds, declared output types, dependencies, and references before calling any factory. Resources follow the plan's topological order. Input references use native SDK outputs, including inside maps and lists. Declared sensitive outputs and secret-tainted resources are marked secret. The returned result retains native typed outputs and exports, and exports are also registered on the Pulumi stack. Factory registration errors deliberately omit provider error bodies, which may contain input values.


See `examples/local` for a complete provider-free Pulumi Go program. With the Pulumi CLI installed, run `pulumi preview` in that directory after selecting a stack and backend. An actual update writes state to the selected Pulumi backend; this repository does not perform one.

Run validation from this directory:

```sh
go test ./...
go vet ./...
```

The tests use the official SDK's `WithMocks` monitor and do not contact a cloud. They check native references, dependency registration, secrecy, exports, and preflight refusal. These mocks provide evidence about SDK registration only. They do not prove provider schemas, credentials, IAM permissions, readiness, bootstrap behavior, real infrastructure deployment, persistent jobs, or recovery. Trusted factories remain responsible for provider-specific schemas and for forwarding the adapter's resource options. Secrets remain plaintext in trusted process memory and must not be logged by factories.
