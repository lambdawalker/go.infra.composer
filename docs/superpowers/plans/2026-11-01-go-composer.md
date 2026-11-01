# Go Composer Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans for the core and independent delegated tasks for configuration/Pulumi after their shared API is fixed.

**Goal:** Implement the tested Go composition core, configuration validation and Pulumi adapter.

**Architecture:** Pure declarations resolve to immutable, typed and secret-aware plans. The simulator and optional Pulumi module consume that boundary. Configuration contracts validate separately before deployment integration.

**Tech Stack:** Go standard library for the core; pinned Pulumi Go SDK in an optional nested module.

**Spec:** ../../design.md

## Global constraints

- Module github.com/lambdawalker/go.infra.composer, package composer; Go 1.25+.
- Do not deploy AWS or claim mocks prove cloud behavior.
- No plaintext secrets in public JSON/diagnostics; trusted adapter Node access is explicit.
- Match contracts before choosing providers; freeze plans; reject creation cycles before execution.

## Review focus

- Defaults expanding the provider set can introduce ambiguity: test final-set re-resolution.
- Inputs may be cyclic/nonfinite/non-JSON Go data: test safe rejection without panic.
- Returning nested maps/slices can break snapshot isolation: mutate accessor results in tests.
- Secret taint can be lost through forwarded references or outputs: test both serialization and adapter marking.
- Failed/cancelled execution must not imply success or silently rerun effects: test zero/partial operations.

## Task 1: Core model, resolver and graph

Files: value.go, model.go, diagnostics.go, application.go, graph.go, plan.go, *_test.go.

- [ ] Write failing contract/provider/graph/snapshot/secret tests against docs/design.md interfaces.
- [ ] Run go test ./... and observe missing API failures.
- [ ] Implement typed values, stable default closure, forwarding, input edges and iterative graph validation.
- [ ] Run tests including permutation, long-chain, hostile data and copy-isolation cases.

## Task 2: Execution and runnable examples

Files: executor.go, examples/authentication, examples/emailvalidation, examples/contracts, cmd/infra-compose.

- [ ] Test invalid plans never call executor, typed simulator results, cancellation and secret-safe output.
- [ ] Implement Executor/Simulator and standalone/integrated independent feature examples.
- [ ] Test CLI human/JSON diagnostics, exit status, environment and deliberately broken scenarios.

## Task 3: Configuration contracts

Files: configuration/*.go and tests. Independent package using the fixed core API.

- [ ] Write failing property/consumer/binding coverage and security/source-priority tests.
- [ ] Implement deterministic validation, source selection and typed binding plans; document reader/writer limits.
- [ ] Run package tests, including secrets, stale requirements, collisions and ambiguous mapping.

## Task 4: Optional Pulumi adapter

Files: adapters/pulumi/go.mod, go.sum, adapter.go, adapter_test.go, README.md.

- [ ] Write failing native mock tests for typed outputs, input dependencies, secret propagation and unsupported kinds.
- [ ] Implement registered resource factories and preflight all kinds before SDK registration.
- [ ] Pin SDK dependencies; run nested module tests and vet without a cloud account.

## Task 5: Integration and review

Files: README.md, .github/workflows/ci.yml and documentation.

- [ ] Run core tests/race/vet, adapter tests/vet and CLI smoke scenarios.
- [ ] Record exact implemented scope and deferred standard subsystems.
- [ ] Obtain independent code review, fix material issues and rerun relevant checks.
- [ ] Commit, push branch, open PR and verify published files/checks.
