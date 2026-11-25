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
| `ArtifactDescriptions` | Optional map from exact Pulumi logical resource names to short, nonsecret purpose descriptions. The wizard and exported reports show them for resources already in state and resources proposed by preview. |
| `Outputs` | Allowlist of nonsecret string outputs for status and persisted output receipts |
| `GitHub` | Workflow and environment prefix. A managed policy name enables reviewed role/policy setup. Without one, GitHub setup uses an existing protected environment, OIDC provider and explicitly supplied role. |
| `DiscoveryStack` | Optional shared index program directory, Pulumi project and stack to remove after all saved environments. |

The manifest describes a single isolated stack per environment. It contains
no cloud credentials, target account IDs or executable shell snippets.
The tool checks the directory's `Pulumi.yaml` project name against the
manifest before opening any deployment records.

`ArtifactDescriptions` keys are the exact logical resource names after the
last `::` in a Pulumi URN, not generated AWS physical names. For example,
`"testhost.app-demo-domain": "Assigns the demo HTTPS hostname to API Gateway."`
describes the resource named `testhost.app-demo-domain` in state and preview.
Keep descriptions short and free of credentials; the wizard also includes them
in exported artifact reports.

Pin a reviewed module version or commit in both launchers. For example,
from a consumer checkout where `infra-deploy` is already installed:

```sh
infra-deploy --action build
infra-deploy --action preview --environment dev
```

Run `--help` from that checkout to see its commands and input flags. Plain
terminal use defaults to deploy. After the interface choice, the wizard asks
for the local vault passphrase before collecting settings. The web wizard asks once
for each unlock. On first use, enter the separate Pulumi passphrase for the stack.
The terminal wizard also confirms a newly created vault passphrase.

The review screen displays nonsecret values before AWS or GitHub sign-in.
Use Tab and Enter to open an editable field. Smaller terminals show a numbered
menu. Choose Continue to sign in and run target checks. Project and environment
select the vault and receipt scope at startup. An existing account, region and
S3 state URL are tied to that scope. Use `--environment` for a different
deployment environment. Ctrl+Q exits during prompts or review. Active external
commands use Ctrl+C. Text input stops the 15-second default timer.

CI uses explicit target arguments, an assumed role and a separate deploy
approval. Deletion, replacement and teardown require deliberate interactive
confirmation.

## Run the browser wizard

In a local desktop terminal, the shared CLI first asks whether to continue in
the terminal or launch the web interface. Press Enter or wait 15 seconds to
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
Vault and stack passphrases use password fields. The **Credentials** panel
accepts Cloudflare and GitHub tokens. After you confirm the AWS account, new
tokens are encrypted locally with the vault passphrase for that project and
environment. Later runs show `*****` as a placeholder. Leave the field blank
to keep the existing token or enter a new value to replace it. Saved tokens
are used for the corresponding tool in preference to its environment token.
Noninteractive runs continue to use their externally supplied credentials. With no `--action`, the
browser presents action cards that explain each operation and selects Preview.
After choosing the project and environment, review and edit the applicable
settings in one form. Continue validates the full form and saves the accepted
project inputs as defaults for that environment. Account, region, and state
location for an existing deployment stay bound to its verified scope.
Choose **Add AWS SSO profile** to run the AWS CLI setup in the launching
terminal. The browser keeps your form edits and refreshes the profile list
when setup finishes. When run without an attached terminal, configure the
profile separately with `aws configure sso`, then choose **Refresh AWS profiles**.
When an AWS SSO session needs renewal, the AWS CLI opens the sign-in page on
a local desktop. You can also use **Open AWS sign-in** in the web wizard,
including when it runs over SSH. Complete the sign-in in your browser to continue.

The web wizard uses a resizable workspace throughout the run. The left pane
tracks progress and deployment context. The editor has Actions, Settings,
Overview, and Process output tabs; the right pane shows artifact details and a
searchable hierarchy. Settings is available from startup with the saved target
summary, and the wizard places its editable configuration form there when needed.
The workspace loads the last saved target and report
before asking for credentials, when available. Those cached artifacts are
marked unchecked until the selected stack is read. During Preview, recorded
state remains visible while Pulumi prepares the plan, then proposed operations
appear in Overview. Pulumi presence is recorded state, not a live AWS check.
Vault passphrase and short confirmation prompts use a blocking dialog. An
inactivity lock keeps the workspace visible behind the dialog.

The fixed bottom bar keeps the current actions in view. The Continue button
displays the remaining countdown beside a Pause button.
Browser prompts with a routine default wait 15 seconds. A deployment settings
form with resolved, valid defaults waits 30 seconds. Typing, clicking, or
scrolling pauses the countdown, as does leaving the tab. The action menu
stays open after returning from an operation. Passwords, account confirmation,
and deployment approvals require explicit input. Provider command diagnostics
remain in the launching terminal. On a failed or stopped action,
**Copy diagnostics** copies the action, stage, error, and recent output from
supported failed commands. It redacts known secrets and common credential
formats. It leaves out sign-in transcripts, command arguments, and raw
configuration or state outputs.

After any action finishes, fails, or is stopped, review the result and choose
**Back to main page** to open the action cards in the same tab. The wizard waits
for the operation to exit and release its local locks before offering another
action. The open web session reuses the vault passphrase across actions.

The action cards include **Remove an environment**, **Remove all environments
and index**, and **Remove GitHub configuration**. Removing one environment
retires it from the discovery index before destroying its Pulumi stack. Removing
all environments verifies the declared shared index and its existing S3 state
URL first, then requests typed confirmation for the project, each environment
stack, and the index stack. It destroys the index after all saved environments
succeed. If any teardown fails, the wizard stops for recovery and a retry.
The shared index requires its own Pulumi passphrase.

Removing GitHub configuration targets the selected environment's saved GitHub
deployment record. It deletes its GitHub environment and its secrets,
variables and protection rules. If this wizard created a dedicated IAM role
and its tags still identify the project, it also removes that role and its
declared inline policy. A preexisting role and the shared OIDC provider stay
available to other deployments.

After 20 minutes of inactivity, the browser wizard locks. Enter the vault
passphrase to resume from the same step. Operations already running continue
while the page is locked. Use **Close wizard** from the action menu or a result,
or press Ctrl+C in the terminal to end the session. If you close the tab, reopen
the URL printed in the terminal while the local process is still running.
Browser mode is interactive and cannot be combined with `--non-interactive`.

## Publish environment discovery

Applications with a shared discovery stack can add a `Discovery` declaration
to the deploy manifest. Map `APIURL` and optional `CognitoIssuer`,
`CognitoUserPoolID`, and `CognitoClientID` to public names in `Outputs`.
Set `ProbeMethod`, `ProbePath`, and `ProbeStatus` for an unauthenticated HTTP
readiness check. The deploy CLI publishes only those mapped outputs after a
successful apply and probe.

Pass the shared URL with `--discovery-index` (or
`INFRA_DISCOVERY_INDEX_URL`) and its signing region with `--discovery-region`
(or `INFRA_DISCOVERY_INDEX_REGION`). The region defaults to the stack region.
The wizard stores the discovery destination for that deployment scope.
Confirmed destroy retires its entry before removing the stack resources.
Preview and setup leave the directory unchanged.

## Compose deployment inputs

Use `Label` for the field name and optional `Help` for instructions shown below
the field in the browser form. The browser shows the resolved value beside a
composed input and recalculates it as you edit. A field derived entirely from
other inputs is read-only. The form validates fields as you edit them and
checks AWS sign-in, account matching, and existing state bucket access on
Continue. If a check fails, the wizard reopens the form with your entries and
highlights the affected field. It saves settings once the target passes these checks.

A field's `Compose.Function` selects a built-in or project function.
`Compose.Parameters` binds the function's named arguments. Each argument
can come from `userInput` (at most one per field), a resolved `field` by
exact name, the selected `environment` (a text or domain parameter), or a
`literal` string. `DefaultInput` belongs to the prompt and
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

## Standalone websites and nested Go modules

An application can opt into the same CLI without moving its backend module:

```json
{
  "BuildWorkingDir": "backend",
  "BuildPackage": "./cmd/lambda",
  "BuildDir": "dist/deploy",
  "StackPrefix": "{project}-",
  "AllowProduction": true,
  "PrepareBuild": {
    "Directory": "deploy",
    "Package": "./cmd/assets",
    "Inputs": ["web", "packages", "assets", "deploy"],
    "Outputs": ["web/dist", "backend/internal/catalog/catalog.json"],
    "Tools": ["node", "npm"]
  },
  "HealthCheck": {
    "APIURL": "siteUrl",
    "ProbeMethod": "GET",
    "ProbePath": "/api/config",
    "ProbeStatus": 200
  }
}
```

These are optional fields added to the existing manifest, not a complete
manifest. `PrepareBuild` executes `go run <Package>` in its repository-relative
`Directory`. The package is trusted repository code: review it like the Pulumi
program. No arbitrary shell command is accepted. It must produce every declared
output and return nonzero on failure. The consumer owns only its application
build recipe, not a fork of the deployment wizard.

The shared CLI caches prepared outputs using source bytes, the Go dependency
graph/toolchain, declared recipe and optional Node/npm versions. Input traversal
excludes `.git`, `.deploy`, `node_modules` and `dist`; output traversal excludes
nothing. Missing/empty outputs, symlinks, tampering and changed inputs invalidate
reuse or fail closed. Declare every source/lockfile and every generated file
that Pulumi consumes. Environment-dependent build configuration must also be a
declared input; ambient environment variables are not a build configuration API.
`--force-rebuild` reruns preparation and Lambda compilation; `clean-cache` clears
both cache receipts. It leaves generated files in place but they are verified or
rebuilt before reuse.

Preview approval checks the Lambda archive, current Go inputs, infrastructure
source/configuration and declared preparation inputs/outputs before apply.
The build lock remains held through preview/approval/apply. A changed prepared
website, config file or source requires a fresh preview. No frontend compiler
runs inside the Pulumi program.

`{project}-` preserves applications whose established stack naming is
`<selected-project>-<environment>`. Literal prefixes keep their existing behavior.
`AllowProduction` is false by default for isolated test hosts; applications must
explicitly opt in. This does not grant apply approval or relax destructive-plan
confirmation. An empty `GitHub.EnvironmentPrefix` preserves existing environment
names such as `dev`, `qa` and `prod` and their OIDC trust/protection rules.

`HealthCheck` reuses the bounded HTTPS readiness probe without requiring a shared
discovery index. Its URL must be an allowlisted stack output. It runs only after
apply; failure reports **applied, readiness failed**. The applied receipt remains
an apply fact, not an end-to-end acceptance claim.
