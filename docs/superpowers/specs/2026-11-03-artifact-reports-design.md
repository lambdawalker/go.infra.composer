# Artifact previews and deployment reports

Approved direction: common code belongs in Composer; auth, verification and card-capture expose feature previews; Attestra composes reports including AWS, GitHub environments and Cloudflare DNS. On every deployment inspect each known artifact against its provider. Between deployments use cached observations by default, with explicit refresh available.

## Design

A dependency-free reporting package owns versioned JSON models, deterministic hierarchy and priority, status/evidence reconciliation, cache persistence and Markdown/HTML rendering. Provider adapters supply allowlisted resource identity and relationships; never dump arbitrary inputs/outputs, policies, secrets, user uploads or raw logs. Presence and planned action are independent fields. An unsuccessful/unsupported check is unknown, never absent or present; cached evidence retains its observation timestamp and source. Pulumi state alone means recorded, not live verified. Replacements distinguish old physical IDs from new planned instances.

For native Pulumi resources use refresh preview (no state mutation) and per-resource completion events for current presence. Partial streams retain only completed observations and identify remaining unchecked resources. Exact child resources must be checked: a parent existing does not prove its policies, routes, files or permissions exist. Supplemental GitHub/Cloudflare checks use their existing read-only APIs. Cache scope includes application, repository, environment, account, region, backend and stack; do not mix scopes or physical IDs.

Native feature preview catalogs are generated from the actual native constructors with explicitly labeled offline mock bindings, not parallel hand-maintained resource lists. Configuration controls optional branches. These catalogs have no deployed physical IDs. Host dependencies are identified as externally supplied, not created by the feature. Live configured Pulumi previews provide authoritative intended changes for an application stack.

Priority: outcome/failures/destructive changes; endpoints/domains; stateful storage/identity/security; compute/API/queues/schedules; operations/deployment integrations; child configuration and deployment-managed S3 objects beneath parents. Runtime user data is excluded. Ordering is deterministic, orphan children stay visible. HTML is one self-contained escaped page, with main-artifact sections and expandable child subsections. Markdown is usable repository documentation; JSON is schema-versioned machine input.

Attestra owns `.cache/infra-composer/` JSON snapshots and `deployment-reports/` timestamped Markdown, JSON and HTML reports; both ignored by git. Each deploy attempt records metadata, start/end times, revision, outcome, checks and partial failures, including early exits where possible. New snapshots replace cached data atomically, with private file permissions. Cached reports never relabel old evidence as fresh. Failure to check does not erase known inventory or hide successful infrastructure changes. Interrupted attempts leave an honest in-progress/partial record.

## Alternatives

Always-live reporting is slower and requires credentials between deployments. State-only reporting cannot establish deployed presence. The selected hybrid performs live checks during deployment and cached reads between deployments.

## Constraints

No live AWS deployments, state changes, teardown or seeding for validation. Feature boundaries remain independent. Core remains standard-library-only. Use fakes, native Pulumi mocks and fixtures for verification. Publish PRs to each affected repository; do not merge without a current request.
