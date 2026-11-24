package awsnames

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		kind Kind
		good string
		bad  string
	}{
		{LambdaFunction, "testhost-app-lambda", "testhost.app-lambda"},
		{LambdaStatementID, "testhost-app-permission", "testhost.app-permission"},
		{DynamoDBTable, "testhost.app-records", "ab"},
		{IAMRole, "testhost.app-role", strings.Repeat("a", 65)},
		{IAMInlinePolicy, "testhost.app-policy", "path/policy"},
		{CloudWatchLogGroup, "/aws/lambda/app", "aws/reserved"},
		{EventBridgeRule, "testhost.app-worker", "a/b"},
		{EventBridgeTarget, "testhost.app-worker-target", "a b"},
		{SecretsManager, "app/testhost.app-secrets", "app secret"},
		{CognitoUserPool, "testhost.app-users", "pool/path"},
		{CognitoAppClient, "testhost.app-android", "bad:client"},
		{APIGatewayAPI, "testhost.app-demo-api", ""},
		{APIGatewayAuth, "testhost.app-auth", "\x00"},
		{APIGatewayStage, "$default", "test.stage"},
		{APIGatewayRoute, "POST /v1/verification/status", "POST v1/verification/status"},
		{DNSHostname, "test.components.example.com", "bad_host.example.com"},
		{DNSRecord, "_token.acm-validations.aws.", "bad..example.com"},
		{SESEmailIdentity, "verify@example.com", "Verify <verify@example.com>"},
		{S3GlobalBucket, "email-test-state-a31", "email-test--x-s3"},
		{S3GlobalBucket, "mail.test-state", "192.168.1.1"},
		{S3GlobalBucket, "mail-test-state", "amzn-s3-demo-state"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			if err := Validate(tc.kind, tc.good); err != nil {
				t.Fatal(err)
			}
			if err := Validate(tc.kind, tc.bad); err == nil {
				t.Fatalf("accepted invalid value %q", tc.bad)
			}
		})
	}
}
