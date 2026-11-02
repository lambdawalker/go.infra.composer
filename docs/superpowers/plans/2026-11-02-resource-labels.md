# Resource ownership labels implementation plan

Goal: apply the approved seven ownership tags, generated description prefix and metadata-only deployment inventory without renaming AWS resources.

Architecture: SDK-independent ownership.Metadata validates identifiers and generates tags/descriptions. The optional Pulumi awslabels package provides a per-resource transformation, a context-scoped feature owner and a tracker that exports identifiers/parent references without resource inputs. Native feature constructors opt into this transformation; application and standalone entry points install the scope. Actual provider Tags fields determine supported tagging; unsupported objects are recorded explicitly. Only mutable description properties on an explicit allowlist are changed.

Tech stack: Go, Pulumi Go SDK, existing AWS SDK constructors and mock monitors.

Spec: approved tagging proposal in this conversation; documented in design.infra.composer/docs/resource-ownership.md.

Global constraints: retain physical names, preserve descriptions and user tags, prohibit reserved ownership overrides, preserve shared/environment distinction, do not log/store raw resource arguments, do not deploy AWS as validation.

Review focus: immutable IAM policy descriptions; legacy SES SDK limitations; deferred tag maps and parent outputs; feature attribution under concurrent registration; secret values in inventory; IAM permissions for tagging; standalone/control-plane coverage.

- [ ] Test and implement ownership.Metadata.Validate/Tags/Description in ownership/metadata.go.
- [ ] Test and implement awslabels.Install/ForFeature/Option/Export in adapters/pulumi/awslabels; mock taggable and inventory-only resources, collisions, descriptions and unknown outputs.
- [ ] Apply options to all native AWS constructors in authentication, verification and app/control-plane; install scopes at entry points. Assert coverage and existing names in native tests.
- [ ] Update deployment IAM/tag bootstrap paths and documentation, then test all affected Go modules and review source.
- [ ] Publish core/adapter pins and integration branches with PRs; do not merge or deploy.
