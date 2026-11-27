# AWS service icons

The Route 53 signpost is custom artwork shared with the Cloudflare DNS visual.
The Lambda Permission certificate is custom artwork for invocation permissions.
The IAM role, ACM validation, and JWT authorizer plaques are custom certificate artwork.
The hanging www sign represents an API Gateway v2 custom domain.
The three-arm stage sign, Cognito app client plaque and EventBridge target mail slot are custom resource icons.
The three-port lever valve is custom artwork for API Gateway v2 API mappings.
The clevis and pin is custom artwork for API Gateway integrations.
The sealed manuscript is custom artwork for Secrets Manager secret versions.
The sextant is custom artwork for API Gateway v2 routes.
The other fourteen SVGs are AWS service icons downloaded from
[AWS Icons](https://aws-icons.com/), which identifies the
[AWS Architecture Icons](https://aws.amazon.com/architecture/icons/) package as
their source. The artwork is owned by Amazon Web Services, Inc.; consult AWS's
icon terms before reusing it elsewhere.

The local wizard serves these files itself. `aws-icons.js` maps known Pulumi and
Composer-recorded AWS resource families to the bundled service icon;
`workspace.js` uses its neutral symbol for unrecognized resource types.
No icon request goes to the catalog at runtime.
