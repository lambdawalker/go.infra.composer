# Artifact reporting

`reporting` and `reporting/pulumicli` use only Go's standard library. They never deploy, mutate state or infer a child's existence from its parent. Provider inputs/outputs, diagnostics, policies and logs do not belong in the report model. Supply only safe identity/display metadata. State readers copy an explicit allowlist and discard all other resource properties, including secret wrappers.

## Model and evidence

The JSON schema version is `reporting.SchemaVersion` (currently `1`). `Report` has `SchemaVersion`, `Scope`, `Metadata`, `CacheLoaded`, `Warnings []string`, and `Artifacts []Artifact`. `Scope` is a comparable struct of strings: `Application`, `Repository`, `Environment`, `Account`, `Region`, `Project`, `Backend`, `Stack`. Fill every applicable field before using cache. `Metadata` has strings `Revision`, `Operation`, `Outcome`, bool `Dirty`, and `StartedAt`, `FinishedAt` (`time.Time`). Warnings should be stable safe codes, never diagnostic text.

`Artifact` string fields are `Key` (unique logical identity, normally Pulumi URN), `Name`, `Type`, `Provider`, `ProviderID` (exact physical identity), `Owner` (creating repository), `Feature` (canonical `ic:feature`), `Parent` (another Key), `PlannedAction`. `Priority int` sorts ascending, then Key; adapters may override the broad `pulumicli.Priority(resourceType string) int` tiers. `Observation` has `Status`, `Source string`, `ObservedAt time.Time`, `Reason`.

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
```

Normalize copies/sorts inventory, keeps the first duplicate Key and normalizes invalid evidence to unknown. Hierarchy deterministically breaks cycles, keeps orphans, and retains known children beneath an orphan parent. SelectFeature performs exact Feature matching and includes parent closure; parents retain their original ownership. HTML is escaped, self-contained, with visible main sections and collapsed child details; Markdown escapes identity/display text. Human formats include scope, revision, timing, counts and attention items. Zero timestamps display `not checked`.

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

ReadState expects `pulumi stack export` JSON (`deployment.resources`). It keeps URN, ID, type, parent, `ic:owner`/`ic:feature` tags, S3 object key display name, and typed service relationships from `bucket`, `apiId`, `role`, `function`, `userPoolId`, `rule`, `secretId`, `domainName`. Missing owner stays empty; hosts should overlay feature/ownership metadata for resources without tags. Stack/provider/component records are omitted. References resolve only to a unique parent of the expected type, even if child configuration shares a bucket's ID. Service parents replace stack/component parents; explicit real-resource parents stay intact. Secret-wrapped/non-string values are ignored.

ApplyPreview expects newline-delimited engine events. Only allowlisted resourcePreEvent metadata establishes an intended action. New-state metadata uses the same safe extraction and managed-resource filter as ReadState: canonical tags and S3 object key may fall back to inputs, while stack/provider/custom:false components are excluded. Parent relationships resolve after the stream is read so children can precede parents, including typed references across omitted component parents. Existing supplied inventory is retained. Existing physical IDs stay untouched; newly planned resources have no physical ID. Partial parsing returns completed results and `ErrInvalidInput`.

ReadRefresh expects **only the output stream of a read-only refresh**, never general update/preview output. On Pulumi v3.273.0, previewActions.OnResourceStepPost substitutes RefreshStep.ResultOp() for the step's refresh operation before sending resourceOutputsEvent. Thus completed `metadata.op` is `same`, `update` or `delete`; no `resultOp` field exists in SDK StepEventMetadata. Same/update requires exact new.id; delete requires exact old.id and no new state. Pre-events, summaries and parent completions prove nothing about a child. Missing IDs and mismatches remain unknown. Successful completions survive later malformed lines or overall command failure; all unchecked artifacts stay unknown. Raw diagnostics are ignored. Engine semantics reviewed against official `pkg/engine/update.go` and `pkg/engine/events.go` v3.273.0; integration callers should validate this contract when upgrading Pulumi.

CollectRefresh executes arguments `refresh --preview-only --non-interactive --json [--stack STACK] --event-log PRIVATE_FILE` through the injected Runner, discards stdout, reads the private 0600 event log and removes it on return. The runner should run the Pulumi executable in Dir and return an error on nonzero exit. Backend/environment authentication is the caller's responsibility. The Runner must also enable Pulumi hidden debug flags with `PULUMI_DEBUG_COMMANDS=true` for `--event-log` on versions that require it. Only this explicit caller action invokes a provider; no built-in runner or shell invocation exists. Failures return stable `ErrRefresh` without raw diagnostics. The returned artifact slice is always usable for a partial report. Existing observation timestamps are replaced for this attempted check, not relabeled as a fresh success.

RunCLI is reusable for feature constructors' safe offline catalogs. It **never executes Pulumi or contacts providers**. Required `--scope FILE` loads a JSON Scope. Optional flags are `--cache-dir DIR`, `--output-dir DIR`, `--state FILE`, `--preview-events FILE`, `--feature CANONICAL_NAME`. It renders Markdown to stdout, optionally writes private `artifacts.md`, `artifacts.json`, `artifacts.html`, and reports unavailable optional evidence as warning codes. Cache is read-only; it does not save offline plans over live evidence. Scope files and catalogs are provided by the host; constructing native feature mocks remains in the optional Pulumi adapter.
