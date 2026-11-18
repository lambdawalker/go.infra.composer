# infra-deploy

A separately versioned Go CLI for standalone AWS feature demos. It shares
Composer's reporting model and Pulumi report readers. A consuming repo keeps
its infrastructure program, one deploy manifest, thin POSIX and Windows
launchers, and its GitHub workflow. Both launchers and CI run the same pinned
CLI. The Composer simulator and runtime packages remain independent of this
module.

## Define a deployment

Place a `deploy.json` at the consumer repository root. The [email verification
fixture](cmd/infra-deploy/testdata/email.json) shows required SES inputs and
a Pulumi default. The [OCR fixture](cmd/infra-deploy/testdata/ocr.json) shows a
benchmark that stores memory settings and verifies remote ownership before
writing identity configuration.

| Manifest fields | Responsibility |
| --- | --- |
| `Version`, `Repository`, `Feature`, `Title`, `Description` | Schema version, canonical GitHub owner/repo, report ownership, and console labels |
| `PulumiProject`, `PulumiDir`, `StackPrefix` | Existing Pulumi project name, relative program directory, and stack prefix |
| `BucketPrefix` | Name prefix for newly authorized S3 state storage |
| `BuildPackage`, `BuildDir` | Go Lambda package and archive directory under `dist/` |
| `Memory`, `StackIdentity` | Optional Lambda memory setting and opt-in ownership verification for programs with a compatible `deploymentIdentity` output |
| `InputsFile`, `Inputs`, `ConfigDefaults` | Optional versioned nonsecret settings, validated input flags, and defaults written only for missing Pulumi keys |
| `Outputs` | Allowlist of nonsecret string outputs for status and persisted output receipts |
| `GitHub` | Workflow and environment prefix. A managed policy name enables reviewed role/policy setup. Without one, GitHub setup uses an existing protected environment, OIDC provider and explicitly supplied role. |

The manifest describes a single isolated stack per environment. It contains
no cloud credentials, target account IDs or executable shell snippets.
The tool checks the directory's `Pulumi.yaml` project name against the
manifest before opening any deployment records.

Pin a reviewed module version or commit in both launchers. For example,
from a consumer checkout where `infra-deploy` is already installed:

```sh
infra-deploy --action build
infra-deploy --action preview --environment dev
```

Run `--help` from that checkout to see its commands and input flags. Plain
interactive use defaults to deploy. Text input stops the five-second default
timer. CI uses explicit target arguments, an assumed role and a separate deploy
approval. Deletion, replacement and teardown require deliberate interactive
confirmation.

## Adopt an existing deployment

Use the existing checkout, project key, environment, AWS profile/account,
region, S3 URL including the state prefix, and Pulumi stack. The CLI uses the
existing central project records under `~/.infra-composer/<project>/`, and
the same versioned, path-bound encrypted vault under the ignored `.deploy/`
directory. It reads the existing feature settings and recovery receipts.
Preserve recovery copies. A change of launcher does not require stack export,
state import or key rotation. If the project, account or backend is different
from a saved target, stop and investigate the mismatch.

Release the existing wizard's lock only after confirming the previous run has
ended. Validate adoption with a preview and inspect the target and any
replacements before apply. A local validation or preview alone does not
establish that all provider permissions and live integrations are ready.

Composed applications can reuse these deployment primitives once their
multi-stack dependency, shared-output verification and credential-vault
contracts are supported. The standalone command currently accepts one target
per invocation.

## Verify locally

```sh
go test -race ./...
go vet ./...
go build ./cmd/infra-deploy
```

Run these commands in `deploycli/`. The two feature fixtures exercise the same
engine with fake AWS/Pulumi/Go commands. The tests cover target discovery,
saved plans, explicit CI approval, existing settings, prompt editing and
report sanitization. The tests do not connect to cloud services.
