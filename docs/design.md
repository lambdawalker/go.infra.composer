# Go implementation scope

Implements the composition model in design.infra.composer at d31a00854fc6c48fe0266ec9af5c1b7b3206beed, using the reviewed Python milestone as behavioral reference. The external-review refinements remain proposed; they inform deterministic resolution, secret boundaries and honest evidence.

The first Go implementation delivers a usable library and CLI: independent feature declarations; explicit/automatic/default injection; contract/field/label validation; production policy; resource and runtime graphs; actionable errors; immutable plan snapshots; environment isolation; typed/secret-aware values; strict configuration contracts; a local executor; and a separate Pulumi SDK adapter with native mock tests. It does not implement a hosted dashboard, all provider configuration writers, persistent release control plane, credential vault or remote package registry. These are independent follow-on subsystems, not claimed by a working simulator.

Use a Go library with an explicit executor interface rather than a CLI-only translator. Keep Pulumi in a nested module so the core has no third-party dependencies. Modules choose their standalone defaults; integrating applications select bindings. The CLI demonstrates the API, not an untrusted remote manifest runner.

## Core API contract

Module: github.com/lambdawalker/go.infra.composer; root package composer. Go 1.25+ for the core; the pinned adapter requires Go 1.27.3+. Declarations are plain Go data; do not edit them concurrently with planning. Plans copy and own all nested data; accessors return detached copies. A successful Plan freezes Application injection. Rebuild an Application to change the composition.

- Type string constants String, Integer, Number, Boolean. Field{Type Type, Sensitive bool}. Contract{ID string, Major int, Fields map[string]Field}.
- Environment{Name, Account, Region string; Production bool}; prod/production always imply production.
- Value has private representation. Literal(any), ResourceRef(resource, field string), RequirementRef(feature, requirement, field string), Object(map[string]Value), List(...Value), Secret(Value) return Value. Values contain finite JSON-like scalars/maps/lists; unsupported objects/cyclic containers fail planning. Secret values print/JSON-encode redacted. Node() returns a detached trusted-adapter Node{Kind string, Literal any, Resource string, Field string, Secret bool, Object map[string]Value, List []Value}; kinds literal/reference/object/list. Requirement references never survive successful planning. Node explicitly exposes plaintext to trusted adapters, not public serialization.
- Resource{ID, Kind, Source string; Inputs map[string]Value; Outputs map[string]Field; DependsOn []Dependency}. Resource IDs are local within features. References use Feature.resource. Dependency{Resource, Reason, Source string}.
- Offering{ID, Source string; Contract Contract; Values map[string]Value; Labels map[string]string; TestOnly bool}.
- Requirement{ID, Source string; Contract Contract; Labels map[string]string; Default *Default}. Default{Feature *Feature, Offering string}.
- RuntimeUse{Requirement, Reason, Source string}. Feature{ID, Source string; Resources []Resource; Offerings []Offering; Requirements []Requirement; Runtime []RuntimeUse}.
- NewApplication(name string, features ...*Feature) *Application. Inject(feature, requirement, provider, offering string) error; DisableDefaults() error. Plan(Environment) (*Plan,error). Explicit injected providers must be registered in the Application (no hidden package lookup); defaults add their owner's feature.
- Diagnostic{Code, Message, Source, Suggestion string; Path []string; Details []string}; ValidationError{Diagnostics []Diagnostic; DeploymentStarted bool} implements error. No input/secret values in errors.
- Plan.Name() string, Environment() Environment, Resources() []PlannedResource, Order() []string, Exports() map[string]Value, Bindings() []Binding, RuntimeDependencies() []RuntimeDependency. PlannedResource{ID, Kind, Source string; Inputs map[string]Value; Outputs map[string]Field; Dependencies []Dependency}. Binding{Consumer, Provider, Contract, Origin string}; RuntimeDependency{Consumer, Provider, Reason, Source string}. MarshalJSON safely serializes plan metadata/values. Snapshots are detached.
- Executor interface Execute(context.Context,*Plan) (*Result,error). Application.Deploy(ctx,env,executor) plans before calling an executor. NewSimulator() *Simulator implements it. Result{Operations []Operation, Exports map[string]Value}; Operation{Resource string, Inputs, Outputs map[string]Value}. Results redact secrets when serialized/formatted. No persistence/idempotency claim. Cancellation checked before operations. Invalid composition never calls executor.

## Resolution and validation

Explicit bindings first; otherwise unique contract/major/required-field-type/label compatible provider; otherwise enabled default. Expand defaults in stable rounds to a fixed point; re-evaluate final candidates. Ambiguity is an error, never highest/order wins. Explicit incompatibility does not fall back. A selected test-only provider fails in production. Validate duplicate IDs/owners, missing fields, invalid values and references. Defaults and forwarded requirement fields can introduce cycles; diagnose forwarding separately.

Build resource dependencies from explicit edges and references nested in objects/lists. Preserve reason/input path and source. Iterative DFS with sorted nodes/edges emits closed cycle paths. RuntimeUse never adds an implicit creation edge. A plan uses typed outputs; conservatively taint resource outputs if any input is secret and propagate taint through references, exports and simulation. Declared sensitive contract/output fields are also protected. Untrusted serialized plans are not accepted by an executor API.

## Configuration and Pulumi boundaries

The configuration package validates complete property/consumer/binding coverage with application/artifact categories, independent sensitivity, supported sources, initial/default policy, source location, identities and freshness. Reject gaps, unknowns, collisions, insecure secret delivery, unsupported readers and conflicting writers. Security/functional eligibility precedes provider rank and cost; explicit bindings cannot bypass it. It produces a binding plan, not a claim that AWS credentials/permissions exist.

The Pulumi adapter consumes a valid Plan, validates all resource kinds before registration, maps typed references to Pulumi Outputs, preserves dependencies and secret marking, and exports results through SDK Outputs. Unsupported kinds fail before resource registration. Use a bounded kind registry supplied by Go application code; no browser-supplied raw constructors. Mocks prove registration semantics only. No AWS cloud deployment is performed here.

## Validation and delivery

Behavioral tests cover Python milestone cases plus default-expansion ambiguity, declaration-order invariance, nested refs, forwarding chains/cycles, long graph chains, defensive copies, invalid container data, secret redaction/taint, cancellation, configuration gap diagnostics and native Pulumi mock outputs/dependencies. Run core tests/race/vet and adapter tests/vet, demo CLI scenarios and CI. Push a reviewable PR; merge only when requested.
