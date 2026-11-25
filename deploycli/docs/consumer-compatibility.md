# Card Designer extension: consumer compatibility

Audit: 2026-11-25. Scope is PR #50 relative to `f00ded86d0a2`.
No consumer dependency pins were changed except Card Designer's proposed PR.

Two potentially disruptive defaults found during review were removed:

- `RefreshBeforePreview` defaults false. Only opt-in manifests add `--refresh`;
  existing previews retain their existing provider read/reconciliation behavior.
- `BindDeploymentInputs` defaults false. Existing deployments retain saved-plan
  and Lambda archive checks without a new recursive filesystem scan. A declared
  `PrepareBuild` also opts into the extended checks because those outputs must be
  bound to approval. Card Designer explicitly enables both options.

`BuildWorkingDir`, `PrepareBuild`, `HealthCheck`, project-derived stack naming,
production support and empty GitHub prefixes are all explicit manifest options.
An omitted option does not reinterpret saved records, vaults or stack names.
The shared core, configuration, ownership, reporting, discovery and Pulumi adapter
source and module manifests are byte-identical to the PR base.

| Consumer inspected | Revision | Dependency and result |
| --- | --- | --- |
| go.apexfission.aws.email-verification | 16bc4aeaf65e | CLI pinned to f00ded86d0a2; actual unchanged manifest loads and candidate Lambda build passes. Its `test-` stack prefix, discovery, composed sender and GitHub prefix remain intact. |
| go.appexfission.aws.card-capture | 714163352567 | CLI pinned to cfce9564fec2; actual unchanged manifest loads and candidate Lambda build passes; `ocr-` stacks, memory and ownership settings remain intact. |
| go.attestra.aws | b4e087bc0631 | Core module pin; own deployment orchestration. No affected module code changed. |
| go.apexfission.aws.auth | 42cbb957a34e | Core/Pulumi module pins. No affected module code changed. |
| go.apexfission.aws.signature | 915da0b0ee61 | Core/Pulumi module pins. No affected module code changed. |
| go.apexfission.aws.verifyemail | 80a378e39926 | Core/Pulumi module pins. No affected module code changed. |

Reproducible regression coverage lives in `compatibility_test.go`, using the actual
email/OCR manifests in `testdata/consumers/`. It checks default options, unchanged
preview arguments, root builds, stack identity, production restrictions and that
legacy approval does not traverse the new filesystem digest path. Existing
mocked CLI flow tests cover build/setup/status/preview/deploy, failures and saved
plan binding. Extended source mutation checks run with explicit opt-in.

Validation: `go test -race ./...`, `go vet ./...` in deploycli; candidate binary
loaded both consumer manifests; email and OCR Lambda builds succeeded; repeated email build reused its verified cache. The core/adapter
consumer result is based on dependency inspection and an empty source diff, not
on claiming to rerun all of those repositories' suites. Existing pinned versions
cannot change merely because this PR merges. Cloud preview/apply and live
acceptance were not performed; this is compatibility evidence, not a guarantee
against unrelated provider/runtime failures.
