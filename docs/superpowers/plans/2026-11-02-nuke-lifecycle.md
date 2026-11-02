# Nuke lifecycle implementation plan

**Goal:** Reuse the all-environment nuke workflow in Composer, including shared admin/index, persistent reporting, full verification and remaining-only checks.

**Architecture:** A dependency-free `teardown` engine owns discovery, retry inventories, phase ordering and reports. The `teardown/pulumicli` adapter uses installed Pulumi/AWS CLIs through a bounded runner. A separate `cmd/infra-nuke` command leaves the existing simulator command unchanged.

**Constraints:** Go 1.25 core; no third-party core dependencies; no live infrastructure deletion during validation; discover within explicitly configured application projects/backends; authoritative absence succeeds; denial and unsupported resources remain unverifiable. Recovery buckets/keys run last and require prior cleanup and recovery preflight to succeed.

## Tasks

- [x] Implement and test the reusable engine and private atomic file store. Capture identifiers before destruction, preserve inventories across retries, continue independent environments, and process shared stacks last.
- [x] Implement and test the Pulumi/AWS CLI adapter. Validate account/project ownership, preserve encrypted state while removing deletion guards, verify individual resources, and scope recovery deletion explicitly.
- [x] Implement and test the real CLI with typed application/account confirmation, report locking, readable type/region/status reporting and full/fast checks. Fast mode reads the latest checks and only queries REMAINS entries.
- [x] Document the API, configuration, discovery boundary, recovery gate and unsupported-resource behavior. Extend CI to build the new command.
- [x] Run core/adapter tests, race detection, vet and Linux/Windows builds; obtain independent review; push a PR.

## Review focus

- Wrong-account or foreign-project inventories must not authorize deletion.
- Missing stacks with no surviving inventory must not certify absence of cloud orphans.
- Report-save failure must stop destructive work before inventories can be lost.
- A failed or unverifiable environment must preserve all recovery state.
- Full/fast checks must never discover new targets or call deletion methods; skipped unknown evidence must remain visible.
