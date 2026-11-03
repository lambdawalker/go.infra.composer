# Artifact Reports Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Preview and document artifacts with honest live/cached evidence and automatic deployment reports.

**Architecture:** Composer reporting model and renderers normalize safe inventory and preview events. Native feature catalogs and Attestra workflow adapters supply inputs and live observations.

**Tech Stack:** Go standard library core, optional Pulumi Go adapter, existing AWS/GitHub/Cloudflare clients.

**Spec:** ../specs/2026-11-03-artifact-reports-design.md

## Global Constraints

Core remains standard-library-only. No live deployment/state mutation/teardown/seeding for tests. Unknown checks never imply absence. Secrets/raw resource properties must not enter reports. Keep features independent. Cache scope and physical identity must match. Every deployment performs fresh artifact checks; standalone reporting defaults to cache.

## Review Focus

- Partial/malformed provider event streams must not mark unchecked resources present.
- Replacement IDs, account/stack switches and stale cache must not transfer presence.
- Unknown parent links and cycles must not drop children or loop renderers.
- Secret wrappers, HTML/Markdown injection and diagnostic text must not leak into output.
- Early failed deployment and external API errors must leave readable partial reports.

### Task 1: Composer report foundation

Files: reporting/model.go, render.go, cache.go, reporting/pulumicli/*.go, focused tests and README.
Interfaces: versioned report, scope, artifact (logical identity, provider ID, owner, parent, priority, planned action), observation status/source/time/reason; deterministic normalize/merge; cache load/save; write Markdown/JSON/HTML. Pulumi state/preview/refresh readers use explicit allowlists; read-only refresh collector via injected runner. Final exact signatures documented in reporting/README.md for downstream tasks.
- [x] Write regression fixtures for hierarchy, scope isolation, redaction, partial refresh and replacement identities; establish failing tests.
- [x] Implement model/render/cache/collectors and reusable CLI support for consuming feature catalogs.
- [x] Run race tests/vet, review task, commit.

### Task 2: Feature previews

Files: optional adapter preview support in adapters/pulumi, each feature infrastructure/preview.go and infrastructure/cmd/preview/main.go, docs and tests.
Interfaces: use Task 1 reporting API, native mock recorder collects safe logical resource metadata. Offline previews are labeled planned and never assign mock IDs as provider IDs; use explicit config input for optional branches. Cache/state reports match scope and identity.
- [x] Test native constructor coverage and optional branches with fixtures.
- [x] Implement reusable recorder and three thin feature entry points; document commands and host dependencies.
- [x] Run tests/vet in each root and infrastructure; review and commit.

### Task 3: Attestra reporting integration

Files: tools/deploy/internal/workflow/reporting*.go, cli.go, deploy.go, shared_deploy.go, bootstrap lifecycle, CI, .gitignore and docs.
Interfaces: Task 1 core types and collector; app adapters enumerate supplemental GitHub environment and Cloudflare DNS records from existing setup configuration, retaining cache identities if unavailable. Preview mode does not deploy/acquire registry leases; cached report mode bypasses cloud auth. Deploy checks use refresh preview for Pulumi resources and direct external reads.
- [x] Test cache-only execution without cloud access, live check invocation on normal/shared/setup/CI flows, failure reports and external unknowns.
- [x] Integrate preview/report options and deployment reporting lifecycle including metadata, all formats and cache.
- [x] Run all four module tests/vet and relevant helper tests; review and commit.

### Task 4: Published dependency integration

Files: consumer go.mod/go.sum and feature report documentation.
- [x] Publish Composer branch/PR and pin its public commit in all consumers (no sibling replacements).
- [x] Publish feature branches/PRs, pin them in app if necessary.
- [x] Verify public dependency builds and standalone HTML/Markdown/JSON examples; independent full review.
- [x] Publish app PR and give merge order and usage commands.
