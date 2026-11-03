# Artifact reporting

`reporting` and `reporting/pulumicli` use only Go's standard library. They never deploy, mutate state or infer a child's existence from its parent. Raw provider inputs/outputs, diagnostics, policies and logs do not belong in the report model. Supply only safe identity/display metadata and the explicitly allowed recorded settings below. State readers copy an explicit allowlist and discard all other resource properties, including secret wrappers.

## Model and evidence

The JSON schema version is `reporting.SchemaVersion` (currently `1`). `Report` has `SchemaVersion`, `Scope`, `Metadata`, `CacheLoaded`, `Warnings []string`, and `Artifacts []Artifact`. `Scope` is a comparable struct of strings: `Application`, `Repository`, `Environment`, `Account`, `Region`, `Project`, `Backend`, `Stack`. Fill every applicable field before using cache. `Metadata` has strings `Revision`, `Operation`, `Outcome`, bool `Dirty`, and `StartedAt`, `FinishedAt` (`time.Time`). Warnings should be stable safe codes, never diagnostic text.

`Artifact` string fields are `Key` (unique logical identity, normally Pulumi URN), `Name`, `Type`, `Provider`, `ProviderID` (exact physical identity), `Owner` (creating repository), `Feature` (canonical `ic:feature`), `Parent` (another Key), `PlannedAction`. `Priority int` sorts ascending, then Key; adapters may override the broad `pulumicli.Priority(resourceType string) int` tiers. `Observation` has `Status`, `Source string`, `ObservedAt time.Time`, `Reason`.

`Artifact.Configuration *Configuration` is optional (`json:"configuration,omitempty"`) and additive within schema version 1; older reports remain valid. `Configuration` has `Source string`, `ObservedAt time.Time`, and `Values map[string]string` (JSON keys `source`, `observedAt`, `values`). Source must be `pulumi-state` and the timestamp must be nonzero. ObservedAt is when the state was read, not live configuration verification. Values hold canonical integer/boolean/enum text under the actual output field names. No provider defaults or units are inferred. Configuration records state-export settings independently of presence: a later refresh can change presence without advancing this timestamp or verifying these settings. Preview preserves existing recorded settings and never creates them from desired outputs.

Only exact resource types and output fields in this table are supported:

| Resource type | Allowed fields | Values |
| --- | --- | --- |
| `aws:lambda/function:Function` | `memorySize`, `timeout`, `reservedConcurrentExecutions` | Finite integer JSON numbers |
| `aws:sqs/queue:Queue` | `visibilityTimeoutSeconds`, `messageRetentionSeconds`, `delaySeconds`, `receiveWaitTimeSeconds`, `maxMessageSize` | Finite integer JSON numbers |
| `aws:cloudwatch/logGroup:LogGroup` | `retentionInDays` | Finite integer JSON numbers |
| `aws:dynamodb/table:Table` | `readCapacity`, `writeCapacity` | Finite integer JSON numbers |
| `aws:dynamodb/table:Table` | `deletionProtectionEnabled` | JSON boolean |
| `aws:dynamodb/table:Table` | `billingMode` | Plain string `PROVISIONED` or `PAY_PER_REQUEST` |

Configuration extraction reads outputs only, with no inputs fallback. Unsupported types/fields, wrappers, arrays, nulls, numeric/boolean strings and arbitrary strings are omitted. No environment variables, policies, tags, user data, URLs, credentials, arbitrary nested properties, S3 contents or lifecycle documents are copied. Empty/invalid configurations are omitted. Normalize deep-copies and sanitizes configuration, including caller-supplied maps, against the same allowlist. HTML and Markdown label this block “Recorded configuration” with its independent source and timestamp, followed by settings in field-name order.

Status constants are `Unknown`, `Recorded`, `Present`, `Absent` (`reporting.Status`). Reason constants are `Unchecked`, `CheckFailed`, `InvalidEvidence`, `NotFound`, `IdentityChanged` (`reporting.Reason`); empty reason means no issue. State only supplies `Recorded`. A successful exact provider check supplies `Present` or `Absent`. Failed/unsupported/malformed/unchecked checks supply `Unknown`. A planned replacement's old physical instance may remain present: `PlannedAction` and observation describe different facts. Never copy old-ID evidence to a new or empty ID. Offline mocks must leave ProviderID empty and presence unknown.

```go
func Normalize(Report) Report
func Merge(current, cached Report) Report
func Hierarchy([]Artifact) []Node // Node{Artifact Artifact; Children []Node}
func SelectFeature([]Artifact, string) []Artifact
func CachePath(dir string, scope Scope) string
func LoadCache(path string, scope Scope) (Report, error)
func SaveCache(path string, report Report) error
func WriteJSON(io.Writer, Report) error
func WriteMarkdown(io.Writer, Report) error
func WriteHTML(io.Writer, Report) error
func NewStateConfiguration(resourceType string, outputs map[string]json.RawMessage, at time.Time) *Configuration
```

Normalize copies/sorts inventory, keeps the first duplicate Key and normalizes invalid evidence to unknown. NewStateConfiguration extracts a safe recorded configuration from state outputs; only ReadState calls it in the adapters. Hierarchy deterministically breaks cycles, keeps orphans, and retains known children beneath an orphan parent. SelectFeature performs exact Feature matching and includes parent closure; parents retain their original ownership. HTML is escaped, self-contained, with visible main sections and collapsed child details; Markdown escapes field labels and identity/display text. Human formats include scope, revision, timing, counts and attention items. Zero timestamps display `not checked`.

Merge only reuses cached evidence when scope, Key, Provider and nonempty ProviderID match and the current artifact has no attempted observation. An explicit current failed/unknown observation supersedes cached success. Cached-only inventory remains visible (with its planned action cleared). Callers with an authoritative complete new inventory may filter cached-only entries themselves before merging. Merge sets CacheLoaded on equal scopes; LoadCache sets it on success. Sources/timestamps stay unchanged: this boolean means a snapshot was loaded, not that its evidence is fresh. SaveCache writes atomically using a private temporary file and 0600 destination; directories it creates are 0700. CachePath hashes the entire scope. LoadCache rejects mismatched scopes and unknown schema versions.

## Pulumi CLI adapters

```go
func ReadState(io.Reader, time.Time) ([]reporting.Artifact, error)
func ApplyPreview([]reporting.Artifact, io.Reader) ([]reporting.Artifact, error)
func ReadRefresh(io.Reader, []reporting.Artifact, time.Time) ([]reporting.Artifact, error)

type Runner interface {
    Run(context.Context, string, []string, io.Writer) error
}
type RefreshOptions struct {
    Dir string
    Stack string
    ObservedAt time.Time // zero means current UTC time
}
func CollectRefresh(context.Context, Runner, RefreshOptions, []reporting.Artifact) ([]reporting.Artifact, error)
func RunCLI(args []string, catalog []reporting.Artifact, stdout io.Writer) error
```

ReadState expects `pulumi stack export` JSON (`deployment.resources`). It keeps URN, ID, type, parent, `ic:owner`/`ic:feature` tags, S3 object key display name, and typed service relationships from `bucket`, `apiId`, `role`/`roleArn`, `function`/`functionArn`, `userPoolId`/`userPoolArn`, `rule`/`ruleArn`, `secretId`/`secretArn`, `domainName`. Missing owner stays empty; hosts should overlay feature/ownership metadata for resources without tags. Stack/provider/component records are omitted. References resolve only to a unique parent URN of the expected type, even if child configuration shares a bucket's ID. Exact provider IDs and plain-string outputs.arn are internal lookup aliases: this supports Lambda permissions referencing an ARN while the function ID is its name. Aliases are deduplicated by URN and never persisted as extra report properties; secret-wrapped ARNs are ignored. Service parents replace stack/component parents; explicit real-resource parents stay intact. Secret-wrapped/non-string values are ignored.

ApplyPreview expects newline-delimited engine events. Only allowlisted resourcePreEvent metadata establishes an intended action. New-state metadata uses the same safe extraction and managed-resource filter as ReadState: canonical tags and S3 object key may fall back to inputs, while stack/provider/custom:false components are excluded. Parent relationships resolve after the stream is read so children can precede parents, including typed references across omitted component parents. Existing supplied inventory is retained. Existing physical IDs stay untouched; newly planned resources have no physical ID. Partial parsing returns completed results and `ErrInvalidInput`.

ReadRefresh expects **only the output stream of a read-only refresh**, never general update/preview output. On Pulumi v3.273.0, previewActions.OnResourceStepPost substitutes RefreshStep.ResultOp() for the step's refresh operation before sending resourceOutputsEvent. Thus completed `metadata.op` is `same`, `update` or `delete`; no `resultOp` field exists in SDK StepEventMetadata. Same/update requires exact new.id; delete requires exact old.id and no new state. Pre-events, summaries and parent completions prove nothing about a child. Missing IDs and mismatches remain unknown. Successful completions survive later malformed lines or overall command failure; all unchecked artifacts stay unknown. Raw diagnostics are ignored. Engine semantics source-reviewed against official `pkg/engine/update.go` versions v3.264.0 (the app CI pin) and v3.273.0, with `pkg/engine/events.go` v3.273.0; integration callers should validate this contract when upgrading Pulumi. This compatibility evidence is source review, not live CLI/provider execution.

CollectRefresh executes arguments `refresh --preview-only --non-interactive --json [--stack STACK] --event-log PRIVATE_FILE` through the injected Runner, discards stdout, reads the private 0600 event log and removes it on return. The runner should run the Pulumi executable in Dir and return an error on nonzero exit. Backend/environment authentication is the caller's responsibility. The Runner must also enable Pulumi hidden debug flags with `PULUMI_DEBUG_COMMANDS=true` for `--event-log` on versions that require it. Only this explicit caller action invokes a provider; no built-in runner or shell invocation exists. Failures return stable `ErrRefresh` without raw diagnostics. The returned artifact slice is always usable for a partial report. Existing observation timestamps are replaced for this attempted check, not relabeled as a fresh success.

RunCLI is reusable for feature constructors' safe offline catalogs. It **never executes Pulumi or contacts providers**. Required `--scope FILE` loads a JSON Scope. Optional flags are `--cache-dir DIR`, `--output-dir DIR`, `--state FILE`, `--preview-events FILE`, `--feature CANONICAL_NAME`. It renders Markdown to stdout, optionally writes private `artifacts.md`, `artifacts.json`, `artifacts.html`, and reports unavailable optional evidence as warning codes. Cache is read-only; it does not save offline plans over live evidence. Scope files and catalogs are provided by the host; constructing native feature mocks remains in the optional Pulumi adapter.

The self-contained HTML view has a responsive deployment sidebar with an
application/feature repository table. All artifact cards start collapsed, showing
name, colored text status, a type glyph and short owner. Expanded cards show the
qualified owner and evidence fields. Sort by importance (default), owner, type,
name, status or feature; reverse direction or expand/collapse all. Sorting is
applied within each sibling list so subartifacts remain under their parents.
Search matches an artifact's own name, logical key, qualified owner, type, status
or feature (case-insensitive substring). Status, owner, type and feature dropdowns
use exact values from the emitted inventory, with unique options in UTF-16 code-unit
order and a "Not specified" choice for missing metadata. Search and all selected
filters combine with AND. A matching child keeps its ancestors as context; a
matching parent includes its entire descendant subtree even if those descendants
do not independently match. Ancestor context does not expose unrelated branches.
Counts distinguish direct matches from the cards shown with hierarchy; collapsed
cards retain their disclosure state, so expand them to inspect children. Reset
clears search and filters while preserving sorting and disclosure state. Empty
search results have a distinct message from a report with no artifacts.
Inline JavaScript only handles view controls; no requests or external assets are
needed. Without JavaScript, native disclosure cards still work. Bare owner names
are qualified with the organization of Scope.Repository when available.
