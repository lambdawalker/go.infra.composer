# Resource ownership labels implementation plan

Goal: apply the approved seven ownership tags, generated description prefix and metadata-only deployment inventory without renaming AWS resources.

Architecture: SDK-independent ownership.Metadata validates identifiers and generates tags/descriptions. The optional Pulumi awslabels package provides a per-resource transformation, a context-scoped feature owner and a tracker that exports identifiers/parent references without resource inputs. Native feature constructors opt into this transformation; application and standalone entry points install the scope. Actual provider Tags fields determine supported tagging; unsupported objects are recorded explicitly. Only mutable description properties on an explicit allowlist are changed.

Tech stack: Go, Pulumi Go SDK, existing AWS SDK constructors and mock monitors.

Spec: approved tagging proposal in this conversation; documented in design.infra.composer/docs/resource-ownership.md.

Global constraints: retain physical names, preserve descriptions and user tags, prohibit reserved ownership overrides, preserve shared/environment distinction, do not log/store raw resource arguments, do not deploy AWS as validation.

Review focus: immutable IAM policy descriptions; legacy SES SDK limitations; deferred tag maps and parent outputs; feature attribution under concurrent registration; secret values in inventory; IAM permissions for tagging; standalone/control-plane coverage.

- [x] Test and implement ownership.Metadata.Validate/Tags/Description in ownership/metadata.go.
- [x] Test and implement awslabels.Install/ForFeature/Option/Export in adapters/pulumi/awslabels; mock taggable and inventory-only resources, collisions, descriptions and unknown outputs.
- [x] Apply options to all native AWS constructors in authentication, verification and app/control-plane; install scopes at entry points. Assert coverage and existing names in native tests.
- [x] Update deployment IAM/tag bootstrap paths and documentation, then test all affected Go modules and review source.
- [x] Publish core/adapter pins and integration branches with PRs; do not merge or deploy.


Validation: core and optional adapter suites, both native feature suites, application backend/control-plane suites and deployment-tool suite passed with race detection and vet. Native feature/application suites also passed with GOWORK=off against pinned module packages. Runtime/root suites, five Node admin tests and nine Python helpers passed. The private verification module was supplied via the connected source and a temporary local module proxy; normal CI still needs private-repository read access. Review findings (IAM description charset, two missing parent links, JSON encoding of existing IAM tags) were fixed and covered by regressions. No AWS deployment, preview against a live account, renaming or live tagging was performed.
