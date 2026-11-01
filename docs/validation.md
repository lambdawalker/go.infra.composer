# Validation record

The Go library follows [design.infra.composer](https://github.com/lambdawalker/design.infra.composer/tree/d31a00854fc6c48fe0266ec9af5c1b7b3206beed). It extends the local Python milestone into typed/secret-aware plans, static configuration contracts and an optional native Pulumi adapter.

## Executed checks

On 2026-11-01, 37 top-level core/configuration/CLI/example tests (65 cases including subtests) and 4 native Pulumi tests passed:

- Core, configuration, examples and CLI tests passed on Go 1.25.0 and Go 1.27.3.
- Core module `go test -race ./...` and `go vet ./...` passed on Go 1.27.3.
- Optional Pulumi module `go test -race ./...` and `go vet ./...` passed with SDK v3.273.0 on Go 1.27.3.
- Formatting, whitespace and local documentation links were checked.
- Integrated/standalone CLI behavior and deliberate failure scenarios are covered by tests; CI builds the CLI independently.

## Behavior covered

| Area | Evidence |
|---|---|
| Matching/defaults | Explicit priority, incompatibility without fallback, unique matches, labels, defaults disabled/unused, final-set ambiguity and production rejection |
| Dependencies | Nested input edges, self/mutual/long cycles, runtime relationships, iterative 2,500-resource chain traversal, forwarded references and composite-cycle rejection |
| Plan isolation | Detached maps/slices, declaration mutation after planning, registration-order invariance and injection freeze |
| Values/secrets | Finite typed values, hostile/cyclic container rejection, nested secret wrapping, copied-plan formatting, contract sensitivity and originating-output protection |
| Execution | No executor call for invalid composition, typed repeatable simulation, cancellation for resource/empty/export-only plans and secret-safe results |
| Configuration | Full coverage, initial/default distinctions, unavailable-source policy, reader support, scopes/identities, source security constraints, ambiguity, freshness, collisions and writer conflicts |
| Native Pulumi | Official WithMocks registration, typed deferred outputs, nested inputs, dependencies, secret outputs/exports, unknown-kind preflight and nil/zero-plan rejection |

An independent code review identified nested forwarding recursion, formatting and sensitivity edge cases, and cancellation of empty plans. Each material finding received a regression reproduction before its fix; the full suites were rerun afterward. Sensitivity requirements now reject publicly declared offerings unless the offered value is explicitly protected. Sensitive references mark their originating resource output, which the adapter forwards using additional secret outputs.

## Limits

No AWS deployment, live permission/readiness check or production data operation was performed. Mocks do not prove provider behavior. Static configuration selection does not provision readers/writers or IAM. Local simulation has no durable job/state, encrypted credential store, release routing or recovery guarantee. Tests cover represented declarations and trusted factories; arbitrary code inside a Go feature/factory remains trusted code.

CI independently checks core Go 1.25.x/1.27.x and the optional adapter's pinned toolchain. The standard's hosted admin/index, provider-specific live settings, bootstrap/readiness and persistent release/control-plane requirements remain explicit future milestones.
