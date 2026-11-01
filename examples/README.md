# Independent feature examples

`authentication` and `emailvalidation` share only the contracts in `contracts`.
Neither feature imports the other. Each feature supplies its own test-only
standalone default, which production policy rejects. The integrated application
discovers the two real offerings automatically and needs no default providers.
Applications can instead select the same providers with explicit `Inject` calls.

Authentication uses the email validator at runtime. Email validation needs the
user pool during resource creation, so the creation order is pool, then validator.
The runtime dependency does not add a reverse creation dependency.

From the module root:

```sh
go run ./cmd/infra-compose
go run ./cmd/infra-compose -scenario auth -plan-only
go run ./cmd/infra-compose -scenario email -json
go run ./cmd/infra-compose -scenario integrated -environment production -json
```

Every execution is a **LOCAL SIMULATION**. The example resource kinds are local
demonstrations; no cloud provider is contacted and no cloud resources are created.
`-account` and `-region` supply metadata, not credentials or connectivity.

`self`, `mutual`, and `long` deliberately introduce resource cycles. `missing`
has an unsatisfied contract; `ambiguous` has two eligible providers. These scenarios
exit 1 before simulation. With `-json`, validation errors are structured JSON on
stderr. Invalid flag syntax or an unknown scenario exits 2. `-plan-only` validates
and prints a plan without executing the simulator.
