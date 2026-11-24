# AWS environment discovery

This optional Pulumi module provisions one shared environment index per
application. The public `GET /v1/environments` response is versioned and lists
published environment IDs, HTTPS API origins, and optional public Cognito
configuration. The registry Lambda writes to a protected DynamoDB table and
requires IAM authorization on the update route.

Build `cmd/registry` as an ARM64 Linux Lambda executable named `bootstrap`.
Pass its archive, a DNS hostname, provider, and public zone ID to `Deploy`.
Use the returned URL for the app and for the deployment publisher. Use the
execution ARN to grant each publisher `execute-api:Invoke` on its environment's
`POST /v1/environments/<environment>/changes` route. Keep the index stack in
the publisher's account and configure its actual AWS region for signing.

The shared deploy CLI can publish automatically when the app declares its
output mapping and readiness probe. Revisions fence stale publishers and
retirement requests. The handler keeps the last published configuration
available while a deployment is running.
