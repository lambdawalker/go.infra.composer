# Configuration contracts

`configuration.Validate` checks an application's complete logical property,
artifact consumer and source binding inventory before execution. Application and
artifact categories describe meaning; sensitivity is a separate required field.
Every declared consumer needs an explicit binding, including optional properties.
Properties without consumers and bindings without consumers are errors.

```go
initial := composer.Literal("https://service.example")
plan, err := configuration.Validate(configuration.Declaration{
    Environment: "dev", PolicyVersion: "my-application-policy-v1",
    Properties: []configuration.Property{{
        Name: "service_url", Type: composer.String,
        Category: configuration.Application, Sensitivity: configuration.Public,
        Required: true,
    }},
    Consumers: []configuration.Consumer{{
        Artifact: "handler", Property: "service_url", Readers: []string{"env"},
    }},
    Bindings: []configuration.Binding{{
        Artifact: "handler", Property: "service_url", Initial: &initial,
        OnMissing: configuration.MissingError,
        OnUnavailable: configuration.UnavailableError,
        Sources: []configuration.Source{{
            Name: "deployment-environment", Storage: "environment", Reader: "env",
            Location: "handler", Key: "SERVICE_URL", Scope: "dev",
            ReaderIdentity: "handler-role", WriterIdentity: "config-worker",
            UpdateMethod: "configuration-rollout",
        }},
    }},
})
_ = plan
_ = err
```

An initial value seeds managed storage; it never becomes a read default.
`MissingDefault` requires a separate `Default`; `MissingOmit` is permitted only
for optional properties. Unavailable/unauthorized sources must fail, independently
of missing-value policy. There is no runtime source fallback. Initial/default
values are scalar `composer.Value` values or protected resource references;
confidential values require `composer.Secret` and encrypted scoped secret delivery.
Plaintext environment delivery of confidential properties is rejected.

For remote arrangements, specify a positive `MaxStalenessSeconds`. A property's
positive maximum bounds every selected arrangement. Each source needs a storage
location, key, environment scope, reader identity, writer identity and update
method. Reader support is explicitly declared per artifact. Environment keys
cannot have two logical owners; a shared storage key cannot have conflicting
writers.

An explicit binding has exactly one source. With `Auto: true`, supply source
candidates describing actual complete storage/delivery arrangements. Eligibility
is enforced first, including `RequiredControls`, reader support, identities,
scope, secret delivery and freshness. Among eligible candidates, a strict superset
of controls (including protected delivery) dominates a subset. Incomparable
maximal security arrangements fail as ambiguous; this intentionally avoids a
universal numeric security score. Equal-control candidates are ranked by lower
`ProviderRank`, then lower known `EstimatedCost`. Ties and unknown relative costs
fail as ambiguous. Candidate names or declaration order never break ties. Costs
must use the same workload, region and time basis under the named application
policy; no prices or provider recommendations are built into this package.

The resulting plan owns its metadata and exposes detached bindings. JSON and
normal formatting redact protected initial/default values through composer.Value.
Rejected candidates include metadata-only reasons. Validation errors never print
initial/default material.

This package is a static contract validator and binding planner. Reader clients,
SDK/extension lifecycle, credentials, permission provisioning/effective-access
checks, runtime caching, retrieving missing values, writer APIs, rotation,
rollout/revision control, admin authorization and provider recommendation policies
are **not implemented**. Controls and identities are declarations for trusted
application policy and runtime adapters, not evidence that cloud access exists.
`Property` and `Source` contain no fallback or arbitrary raw provider writer API.
