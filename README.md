# go.infra.composer

Compose independent backend features in Go through contracts, provider injection and validated resource plans.

Implements the first Go milestone of [design.infra.composer](https://github.com/lambdawalker/design.infra.composer). The core has no third-party dependencies. An optional module integrates with the Pulumi Go SDK. **The default CLI executes a local simulator; it does not deploy AWS or run application handlers.**

## Run locally

Go 1.25+ for the core; Go 1.27.3+ for the pinned Pulumi adapter.

```sh
go run ./cmd/infra-compose --scenario integrated --environment qa
go run ./cmd/infra-compose --scenario auth --environment dev
go run ./cmd/infra-compose --scenario email --environment dev
go run ./cmd/infra-compose --plan-only --json
go test -race ./...
go vet ./...
```

The integrated example uses both features. Standalone examples use isolated test-only defaults; production refuses those providers. Deliberate failure scenarios include `self`, `mutual`, `long`, `missing` and `ambiguous`:

```sh
go run ./cmd/infra-compose --scenario mutual --json
go run ./cmd/infra-compose --scenario auth --environment prod
```

The CLI exits 1 for composition/execution failure and 2 for invalid arguments. `go run` itself wraps the program's exit status; a built binary exposes it directly. JSON diagnostics go to stderr; successful plans/results go to stdout.

## Compose features

```go
package main

import (
    "context"
    "fmt"

    composer "github.com/lambdawalker/go.infra.composer"
    "github.com/lambdawalker/go.infra.composer/examples/authentication"
    "github.com/lambdawalker/go.infra.composer/examples/emailvalidation"
)

func main() {
    app := composer.NewApplication("backend", authentication.Feature(), emailvalidation.Feature())
    // Both packages understand neutral contracts, not each other's implementation.
    err := app.Inject("authentication", "email-validator", "emailvalidation", "validator")
    if err != nil { panic(err) }
    err = app.Inject("emailvalidation", "users", "authentication", "users")
    if err != nil { panic(err) }

    result, err := app.Deploy(context.Background(), composer.Environment{Name: "qa"}, composer.NewSimulator())
    if err != nil { panic(err) }
    fmt.Println(result) // Secret-aware public representation.
}
```

Feature declarations contain resources, offerings, requirements and optional defaults. Resource inputs use `Literal`, `ResourceRef`, `RequirementRef`, `Object` and `List`; `Secret` carries sensitivity. Explicit bindings win; otherwise exactly one compatible registered provider is selected before an enabled default. Final-set ambiguity is an error. Explicit providers must be registered with the application; no implicit repository lookup occurs.

Contract matching checks capability identity, exact positive major, required scalar fields/types, sensitivity and selecting labels. Minor/patch range solving is not implemented. Unknown resources/outputs, missing fields, incompatible/default/test providers, duplicate owners and creation cycles fail before an executor is called. Diagnostics include stable codes, declaration origins/reasons and suggested corrections. Call `composer.Origin()` in modules for source file/line provenance.

Mutual runtime references are allowed; only creation prerequisites must be acyclic. Inputs automatically add edges, including nested references. Explicit prerequisites require reasons. The graph traversal handles long chains without recursive DFS.

Successful plans own copies of all data; accessors return detached views. Application injection/default-policy changes are frozen after a successful plan. Declarations are Go data: do not mutate them concurrently with planning; construct a new application to change composition. Untrusted serialized plans cannot become validated executable plans.

## Configuration contracts

The [configuration package](configuration/README.md) validates the complete property/consumer/binding inventory. It separates Application and Artifact categories from sensitivity, distinguishes initial values from missing-value defaults, rejects insecure or unsupported sources and resolves eligible sources by security controls, provider rank and cost. Ties or incomparable policies fail explicitly.

It produces a binding plan. It does not install runtime readers, create IAM permissions or mutate AWS settings; applications must validate that plan before their deployment/update workflow. There is no silent runtime fallback after access failure.

## Pulumi integration

The [optional adapter](adapters/pulumi/README.md) registers a validated plan through a trusted, bounded Go factory registry. It preserves native typed outputs, explicit/input dependencies and secret marking. It preflights every resource kind before invoking constructors. Use native provider SDK factories for real infrastructure; the included [local component example](adapters/pulumi/examples/local) requires no cloud provider.

The adapter is a nested module so importing the core does not pull in the Pulumi SDK. From `adapters/pulumi`, run:

```sh
go test -race ./...
go vet ./...
```

Tests use official `pulumi.WithMocks`. They verify registration semantics, not cloud permissions, DNS readiness, provider replacement behavior or deployment recovery. No cloud deployment was performed for this milestone.

## Deployment approvals

The dependency-free [`approval`](approval/README.md) package supplies explicit/manual and opt-in timed approval policy. Applications provide prompts and deployment-tool integration. Only routine operations can auto-approve on the policy's own deadline; sensitive/destructive operations, cancellation and input failures always require an explicit decision or stop the operation. The default remains manual.

## All-environment teardown

The dependency-free [`teardown`](teardown/README.md) package coordinates report-backed destruction across every configured application environment, then shared **admin/index** projects. The installed Pulumi/AWS CLI adapter verifies resource absence, continues independent targets after failures, and gates explicitly configured state-bucket/KMS cleanup on successful primary cleanup. A separate `infra-nuke` command provides typed scope confirmation, private reporting and full/remaining-only checks. It performs real teardown when authorized; `infra-compose` continues to simulate deployments.

See the [configuration and commands](teardown/README.md) before running it. Discovery covers configured projects/backends and saved inventories, rather than claiming account-wide orphan discovery. Application-specific resources outside Pulumi need a cleanup integration.

## Secret and execution boundaries

Plans, values and simulator results redact sensitive content when formatted or JSON-encoded. Sensitivity propagates through forwarding, nested inputs, resource outputs and exports; sensitive output references mark their originating native outputs too. `Value.Node()` intentionally exposes plaintext to trusted executors. Do not serialize/log a Node or embed credentials in IDs, source paths, names or URLs. Pulumi secret marking cannot protect a factory that prints plaintext.

The simulator returns deterministic typed fixtures and `local://` string outputs. It does not emulate services, persist deployments or implement idempotent updates. Cancellation returns failure and available progress; it never implies already completed effects were reversed.

## Scope and evidence

Implemented: declaration contracts, matching/injection/defaults, environment policy, reference/graph validation, plan isolation, secret-aware values, local execution, independent examples, CLI, static configuration validation and native Pulumi integration.

Deferred: encrypted credential memory, foundation bootstrap/readiness jobs, distributed deployment/recovery coordination, provider configuration readers/writers and all 16 admin panels, release inventory/client update enforcement, remote feature/package discovery and a general version solver. Report-backed teardown is available separately; these broader standard requirements remain follow-on work. The simulator and mocks do not establish production readiness.

See [implementation design](docs/design.md), [plan](docs/superpowers/plans/2026-11-01-go-composer.md) and [validation record](docs/validation.md). CI tests the core and optional adapter independently.

## Resource ownership

`ownership.Metadata` validates canonical `ic:` tags and portable, idempotent descriptions. The optional Pulumi `awslabels` package applies resource options, isolates feature scopes and exports safe native inventory. See [usage](adapters/pulumi/awslabels/README.md) and the [standard](https://github.com/lambdawalker/design.infra.composer/blob/docs/resource-labels/docs/resource-ownership.md).
