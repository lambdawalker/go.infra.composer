# All-environment teardown

`teardown` is the reusable lifecycle coordinator. It has no cloud SDK or third-party dependencies. The optional [`pulumicli`](pulumicli) package invokes installed Pulumi and AWS CLIs; [`infra-nuke`](../cmd/infra-nuke) exposes that driver as a separate command. The existing `infra-compose` CLI remains a local simulator.

## Run

Copy and edit [`nuke.example.json`](../examples/teardown/nuke.example.json) in the **application repository**. Supply every application and shared admin/index project/backend, the actual project directories, AWS account and regions. Paths are relative to the configuration file. Use the normal AWS login/profile environment and Pulumi secrets-provider credentials; credentials do not belong in this JSON or in the report.

```sh
go run github.com/lambdawalker/go.infra.composer/cmd/infra-nuke@latest -config ./nuke.json -nuke
go run github.com/lambdawalker/go.infra.composer/cmd/infra-nuke@latest -config ./nuke.json -check
go run github.com/lambdawalker/go.infra.composer/cmd/infra-nuke@latest -config ./nuke.json -fast-check
```

When running from this repository, replace the module command path with `./cmd/infra-nuke`. PowerShell supports the same flags. A built executable exposes exit code 0 for success, 1 for incomplete/failed operations, and 2 for invalid arguments/configuration. `go run` wraps the executable's exit code.

Nuke requires typing `nuke <application> <account>` after reviewing the configured scope. It discovers all stacks in each configured project/backend, captures safe resource identifiers before mutation, attempts environments before projects with `Shared: true`, and attempts independent targets despite failures. Shared admin and index may be separate projects; mark both shared. Stop deployments before starting; the local report lock prevents concurrent processes using that report, but is not a distributed deployment lock.

Discovery is scoped to the configuration and the surviving report. This library has no account-wide application registry and cannot discover a forgotten backend by guessing its name. Declare all owned backends. Project ownership and AWS account checks refuse foreign resources or unsafe provider overrides. Unsupported resources cannot be certified absent. Missing stacks with no surviving resource inventory are reported as unverifiable, rather than claiming that unknown cloud orphans disappeared.

## Reports and checks

The default private report is `.infra-composer/nuke.<application>.<account>.json` beside the configuration; `-report` chooses a different path. The report preserves target identities, inventory, teardown results, and later checks. It contains no Pulumi state bodies, resource inputs, credential values or provider error payloads. Treat identifiers and paths as private operational information. Custom drivers must not put secrets in identifiers or `Artifact.Data`.

Console output is one table with status, artifact type, region, environment, physical identifier and detail. `-json` writes the full report to stdout; prompts and errors go to stderr. Individual result timestamps distinguish fresh checks from cached evidence.

| Status | Meaning |
|---|---|
| `DELETED` | Verified present before cleanup and absent afterward |
| `ABSENT` | Authoritative verification says the resource is absent |
| `REMAINS` | The resource still exists |
| `UNVERIFIED` | Access, unsupported types, missing inventory or an incomplete response prevents verification |
| `FAILED` | A recovery deletion failed |
| `RETAINED` | Recovery cleanup was blocked |
| `SCHEDULED` | A KMS deletion request is pending; the key is not yet destroyed |

`-check` reads the saved inventory and verifies every artifact without discovery or deletion. `-fast-check` requires a previous check and only queries artifacts last marked `REMAINS`. It preserves skipped results and their timestamps; skipped `UNVERIFIED` evidence still prevents an overall successful result. A pending KMS key remains present during a check and stays eligible for later fast checks. Full checks can detect resources that reappeared after being absent. Neither check mode invokes recovery cleanup.

## Recovery boundary

Recovery deletion is opt-in through explicit `recovery` entries. Each bucket/key must be exclusive to this application (`recovery: true`, `data.exclusive: "true"`). The driver refuses AWS-managed KMS keys, foreign ownership, unsafe multi-region keys, non-backend buckets, remaining/foreign stacks and unrecognized recovery objects.

Every primary artifact must be verified absent before recovery preflight. All recovery artifacts are preflighted before deletion begins. State buckets are processed before keys, and every bucket must be verified absent before any KMS deletion is scheduled. Missing resources count as success. Errors, unverifiable resources or failed report writes preserve recovery data. KMS deletion uses the seven-day waiting period and reports scheduling separately from actual destruction.

Only explicitly declared recovery resources are removed. Retain shared keys, shared OIDC providers and DNS zones. Cloudflare records, GitHub environments and bootstrap IAM roles outside Pulumi state need application-specific cleanup drivers; they are not inferred from an application name. The bundled driver verifies supported AWS resources from managed Pulumi state and captures auxiliary Lambda log groups and DynamoDB backups. Persistent `table_backups` collection artifacts also detect backups created after table deletion. Non-user backups, including AWS Backup recovery points and retained DynamoDB SYSTEM backups, are not deleted through the user-backup API and block a complete result while present. Custom Lambda log groups need explicit managed-resource support or an application cleanup integration.

Stacks with unfinished Pulumi operations are refused before mutation because their resource inventory is uncertain. A backend bucket or recovery encryption key managed inside a target stack is also refused: separate its ownership so the final recovery gate can control it. Legitimate cross-region AWS providers are supported and their resource regions are captured; provider credential/account overrides remain prohibited. Independent project discovery failures become report entries while other configured projects are attempted.

## Embed

```go
driver, err := pulumicli.New(config, nil) // nil selects the installed CLIs
if err != nil { return err }
engine := teardown.Engine{
    Scope: config.Scope,
    Driver: driver,
    Store: teardown.FileStore{Path: "private/nuke.json"},
    Recovery: config.Recovery,
}
report, err := engine.Nuke(ctx)
// Or: report, err := engine.Check(ctx, false) // full
// Or: report, err := engine.Check(ctx, true)  // latest REMAINS only
```

Import `github.com/lambdawalker/go.infra.composer/teardown` and `github.com/lambdawalker/go.infra.composer/teardown/pulumicli`. Embedding callers own confirmation, process/distributed locking and deployment suspension; the CLI supplies typed confirmation and a local report lock. Use a separate report per application/account. Changing scope cannot reuse a report, and a missing/corrupt report cannot silently authorize a check.

Custom `Driver` implementations provide discovery, safe inventory, target destruction, authoritative per-artifact inspection and recovery removal. Recovery also requires `RecoveryPreflight`; absent preflight blocks removal. Optional auxiliary cleanup runs only for inventoried cleanup artifacts. Do not use raw provider errors as report text. Completed cloud effects cannot be rolled back on cancellation; retry uses the surviving inventory.

Validation uses fake process runners and local files. No cloud teardown is required to run `go test -race ./...`; those tests establish workflow behavior, not live credentials, permissions or provider propagation timing.
