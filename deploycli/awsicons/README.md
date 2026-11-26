# AWS service icons and models

Import `github.com/lambdawalker/go.infra.composer/deploycli/awsicons` to use
the same artwork as the deployment wizard. Pin the `deploycli` module in
the consuming project. The catalog is embedded and works offline.

For example, read the Lambda assets from Go:

```go
catalog := awsicons.Files()
icon, err := catalog.ReadFile("icons/aws/lambda.svg")
// Handle err before using icon.
model, err := catalog.ReadFile("models/aws/lambda.glb")
// Handle err before using model.
```

Each service has an [SVG](icons/aws/) and a matching [GLB](models/aws/).
For browser projects, serve `aws-icons.js`, `/icons/aws/`, and `/models/aws/`
from this filesystem. The script exposes `window.wizardAWSIconFor(type)` and
the CommonJS export `awsIconFor`. For example, `aws:s3/bucket:Bucket` resolves
to `/icons/aws/s3.svg`, with its model at `/models/aws/s3.glb`. An unknown
resource type returns an empty string so the caller can choose a fallback.

See the [icon attribution](icons/aws/README.md) and
[model provenance](models/aws/README.md) when redistributing artwork.
