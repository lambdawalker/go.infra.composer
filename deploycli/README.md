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
| `DefaultProject` | Optional project name suggested on the first interactive run. Defaults to `PulumiProject`. |
| `PulumiProject`, `PulumiDir`, `StackPrefix` | Existing Pulumi project name, relative program directory, and stack prefix |
| `BucketPrefix` | Name prefix for newly authorized S3 state storage |
| `BuildPackage`, `BuildDir` | Go Lambda package and archive directory under `dist/` |
| `Memory`, `StackIdentity` | Optional Lambda memory setting and opt-in ownership verification for programs with a compatible `deploymentIdentity` output |
| `InputsFile`, `Inputs`, `Functions`, `ConfigDefaults` | Optional versioned nonsecret settings, validated input flags, composition functions, and defaults written only for missing Pulumi keys |
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
terminal use defaults to deploy. After the interface choice, the wizard asks
for the local vault passphrase before collecting settings. On first use, it also asks you to
confirm the vault passphrase and enter the Pulumi passphrase for the stack.

The review screen displays nonsecret values before AWS or GitHub sign-in.
Use Tab and Enter to open an editable field. Smaller terminals show a numbered
menu. Choose Continue to sign in and run target checks. Project and environment
select the vault and receipt scope at startup. An existing account, region and
S3 state URL are tied to that scope. Use `--environment` for a different
deployment environment. Ctrl+Q exits during prompts or review. Active external
commands use Ctrl+C. Text input stops the five-second default timer.

CI uses explicit target arguments, an assumed role and a separate deploy
approval. Deletion, replacement and teardown require deliberate interactive
confirmation.

## Run the browser wizard

In a local desktop terminal, the shared CLI first asks whether to continue in
the terminal or launch the web interface. Press Enter or wait five seconds to
stay in the terminal. Headless and SSH sessions skip the question and go
straight to the terminal wizard. CI stays noninteractive.

Use `--terminal` to skip the choice and stay in the terminal, or `--server` to
start the browser wizard directly. Both flags work with `infra-deploy`,
`./deploy.sh`, and `deploy.bat`. Add `--no-open` to `--server` to print the URL
without opening a browser. Build, cache cleanup, and settings export skip the
interface question.

The wizard binds to a random loopback port and uses a private session URL.
Use **Theme** in the header to choose **System**, **Light**, or **Dark**.
New wizard sessions follow your system appearance. A manual choice stays in
the current browser tab when you reload.
Vault and stack passphrases use password fields. With no `--action`, the
browser presents action cards that explain each operation and selects Preview.
After choosing the project and environment, review and edit the applicable
settings in one form. Continue validates the full form and saves the accepted
project inputs as defaults for that environment. Account, region, and state
location for an existing deployment stay bound to its verified scope.
Choose **Add AWS SSO profile** to run the AWS CLI setup in the launching
terminal. The browser keeps your form edits and refreshes the profile list
when setup finishes. When run without an attached terminal, configure the
profile separately with `aws configure sso`, then choose **Refresh AWS profiles**.
AWS sign-in instructions appear when an SSO session needs renewal.

The page follows the deployment stages, shows the resource operations from the
saved Pulumi plan, and displays the outcome. Approvals in browser mode require
an explicit click. Deletion or replacement still requires typing the stack
name. Provider command diagnostics remain in the launching terminal.

Use **Stop wizard** to cancel, and **Close wizard** to shut down the local server
once the result is displayed. Pressing Ctrl+C in the terminal also stops the
wizard. Closing the browser tab alone leaves the local process running.
Browser mode is interactive and cannot be combined with `--non-interactive`.

## Compose deployment inputs

Use `Label` for the field name and optional `Help` for instructions shown below
the field in the browser form. The browser shows the resolved value beside a
composed input and recalculates it as you edit. A field derived entirely from
other inputs is read-only. Continue validates the whole form before saving.

A field's `Compose.Function` selects a built-in or project function.
`Compose.Parameters` binds the function's named arguments. Each argument
can come from `userInput` (at most one per field), a resolved `field` by
exact name, or a `literal` string. `DefaultInput` belongs to the prompt and
supplies raw input when the user accepts the displayed composed default.
Omit it to require input on the first run.

This fragment uses the built-in `emailOnDomain` for the SES sender and
declares a project function for deriving a hostname. Put these entries
alongside the other fields in `deploy.json`.

```json
{
  "Functions": {
    "prefixedDomain": {
      "Parameters": {"Prefix": "text", "Domain": "domain"},
      "Result": "domain",
      "Template": "{{lower .Prefix}}.{{.Domain}}"
    }
  },
  "Inputs": [
    {
      "Name": "Sender", "Label": "SES sender", "Kind": "email",
      "GitHubVariable": "BENCH_SENDER",
      "Compose": {
        "Function": "emailOnDomain",
        "DefaultInput": "verify",
        "Parameters": {
          "Address": {"Source": "userInput"},
          "Domain": {"Source": "field", "Name": "SenderDomain"}
        }
      }
    },
    {
      "Name": "DemoHost", "Label": "Demo host", "Kind": "domain",
      "Compose": {
        "Function": "prefixedDomain",
        "Parameters": {
          "Prefix": {"Source": "literal", "Value": "demo"},
          "Domain": {"Source": "field", "Name": "SenderDomain"}
        }
      }
    },
    {
      "Name": "SenderDomain", "Label": "Sender domain", "Kind": "domain",
      "GitHubVariable": "BENCH_SENDER_DOMAIN"
    }
  ]
}
```

With `asdf.com`, Enter accepts `verify@asdf.com`, `email-verify` becomes
`email-verify@asdf.com`, and a complete address on another domain is rejected.
The derived `DemoHost` becomes `demo.asdf.com`. Input declaration order is
flexible. The CLI resolves dependencies before their consumers and rejects
unknown fields, repeated dependencies, and cycles before prompting.

Project functions receive named string arguments with declared kinds
(`text`, `email`, or `domain`). Their Go text templates evaluate locally
with helpers `lower`, `trim`, `contains`, and `reject`. Use `reject "message"` to
return a validation error. The CLI validates the result against the output
field's kind. Function names and parameter bindings are checked at startup.

A field with no `userInput` binding is derived. Omit its `GitHubVariable`.
For prompted fields, flags and GitHub variables carry the raw input. The CLI
keeps the raw value for future edits and sends the resolved value to Pulumi.
When migrating a plain field to a prompted composition, use
`Compose.UseLegacyValueAsInput` if the previous saved value is valid as raw
input. The wizard prefers a saved raw value once one exists. CI must supply
all prompted values explicitly.

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

## Select a DNS provider

For a project that needs a DNS hosting choice, add an input of kind
`dnsProvider` to its `Inputs` list.

```json
{
  "Name": "DNSProvider",
  "Label": "Who hosts the domain's public DNS?",
  "Kind": "dnsProvider",
  "GitHubVariable": "BENCH_DNS_PROVIDER"
}
```

The wizard offers AWS Route 53 and Cloudflare. Choose the service answering
public DNS for the relevant zone, which can differ from the domain registrar.
The choice is editable before sign-in and remembered per environment. The CLI
stores the canonical ID (`route53` or `cloudflare`) and passes it as the
`dnsprovider` Pulumi configuration key. CI passes it explicitly as
`--dnsprovider`. Selecting the provider only saves the choice and displays
instructions for adding records.

DNS providers live in [`internal/dnsprovider`](internal/dnsprovider), with one
source file per provider and a shared list of selectable providers.
