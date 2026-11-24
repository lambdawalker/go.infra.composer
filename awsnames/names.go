// Package awsnames checks caller-chosen names for AWS resources.
package awsnames

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Kind string

const (
	LambdaFunction     Kind = "Lambda function name"
	LambdaStatementID  Kind = "Lambda permission statement ID"
	DynamoDBTable      Kind = "DynamoDB table name"
	IAMRole            Kind = "IAM role name"
	IAMInlinePolicy    Kind = "IAM inline policy name"
	CloudWatchLogGroup Kind = "CloudWatch log group name"
	EventBridgeRule    Kind = "EventBridge rule name"
	EventBridgeTarget  Kind = "EventBridge target ID"
	SecretsManager     Kind = "Secrets Manager secret name"
	CognitoUserPool    Kind = "Cognito user pool name"
	CognitoAppClient   Kind = "Cognito app client name"
	APIGatewayAPI      Kind = "API Gateway API name"
	APIGatewayAuth     Kind = "API Gateway authorizer name"
	APIGatewayStage    Kind = "API Gateway stage name"
	APIGatewayRoute    Kind = "API Gateway HTTP route key"
	DNSHostname        Kind = "DNS hostname"
	DNSRecord          Kind = "DNS record name"
	SESEmailIdentity   Kind = "SES email identity"
	S3GlobalBucket     Kind = "S3 global namespace bucket name"
)

type rule struct {
	valid       func(string) bool
	requirement string
}

func matches(pattern, requirement string) rule {
	return rule{regexp.MustCompile(pattern).MatchString, requirement}
}

var (
	idCharacters   = "ASCII letters, digits, hyphens, or underscores"
	cognitoName    = matches(`^[A-Za-z0-9_\s+=,.@-]{1,128}$`, "contain 1–128 letters, digits, whitespace, or _+=,.@-")
	eventName      = matches(`^[A-Za-z0-9_.-]{1,64}$`, "contain 1–64 ASCII letters, digits, periods, hyphens, or underscores")
	logGroupName   = regexp.MustCompile(`^[A-Za-z0-9_/#.-]{1,512}$`)
	dnsLabel       = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	dnsRecordLabel = regexp.MustCompile(`^[A-Za-z0-9_](?:[A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?$`)
	bucketName     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	ipv4Name       = regexp.MustCompile(`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`)
	stageName      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	httpMethod     = regexp.MustCompile(`^[A-Z]+$`)
	displayName    = rule{nonemptyText, "be nonempty UTF-8 text without control characters"}
	rules          = map[Kind]rule{
		LambdaFunction:     matches(`^[A-Za-z0-9_-]{1,64}$`, "contain 1–64 "+idCharacters),
		LambdaStatementID:  matches(`^[A-Za-z0-9_-]{1,100}$`, "contain 1–100 "+idCharacters),
		DynamoDBTable:      matches(`^[A-Za-z0-9_.-]{3,255}$`, "contain 3–255 ASCII letters, digits, periods, hyphens, or underscores"),
		IAMRole:            matches(`^[A-Za-z0-9_+=,.@-]{1,64}$`, "contain 1–64 ASCII letters, digits, or _+=,.@-"),
		IAMInlinePolicy:    matches(`^[A-Za-z0-9_+=,.@-]{1,128}$`, "contain 1–128 ASCII letters, digits, or _+=,.@-"),
		CloudWatchLogGroup: {func(s string) bool { return logGroupName.MatchString(s) && !strings.HasPrefix(s, "aws/") }, "contain 1–512 ASCII letters, digits, or _/#.- and avoid the aws/ prefix"},
		EventBridgeRule:    eventName,
		EventBridgeTarget:  eventName,
		SecretsManager:     matches(`^[A-Za-z0-9_/+=.@-]{1,512}$`, "contain 1–512 ASCII letters, digits, or /_+=.@-"),
		CognitoUserPool:    cognitoName,
		CognitoAppClient:   cognitoName,
		APIGatewayAPI:      displayName,
		APIGatewayAuth:     displayName,
		APIGatewayStage:    {func(s string) bool { return s == "$default" || stageName.MatchString(s) }, "be $default or contain 1–128 " + idCharacters},
		APIGatewayRoute:    {validRoute, "be $default or an uppercase HTTP method followed by an absolute path"},
		DNSHostname:        {func(s string) bool { return validDNS(s, false) }, "be a valid fully qualified DNS hostname"},
		DNSRecord:          {func(s string) bool { return validDNS(s, true) }, "be a valid fully qualified DNS record name"},
		SESEmailIdentity:   {validEmail, "be a plain email address"},
		S3GlobalBucket:     {validBucket, "follow the S3 global namespace bucket naming rules"},
	}
)

// Validate checks the syntax of a physical name or a default auto-name base.
// It makes no network calls and does not check availability.
func Validate(kind Kind, name string) error {
	r, ok := rules[kind]
	if !ok {
		return fmt.Errorf("unknown AWS name kind %q", kind)
	}
	if !r.valid(name) {
		return fmt.Errorf("%s %q must %s", kind, name, r.requirement)
	}
	return nil
}

func nonemptyText(s string) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && strings.IndexFunc(s, unicode.IsControl) < 0
}

func validRoute(key string) bool {
	if key == "$default" {
		return true
	}
	method, path, ok := strings.Cut(key, " ")
	return ok && httpMethod.MatchString(method) && strings.HasPrefix(path, "/") &&
		strings.IndexFunc(path, unicode.IsSpace) < 0 && nonemptyText(path)
}

func validDNS(name string, record bool) bool {
	if record {
		name = strings.TrimSuffix(name, ".")
	}
	if len(name) > 253 || !strings.Contains(name, ".") || ipv4Name.MatchString(name) {
		return false
	}
	expression := dnsLabel
	if record {
		expression = dnsRecordLabel
	}
	for index, label := range strings.Split(name, ".") {
		if record && index == 0 && label == "*" {
			continue
		}
		if !expression.MatchString(label) {
			return false
		}
	}
	return true
}

func validEmail(s string) bool {
	address, err := mail.ParseAddress(s)
	return err == nil && address.Address == s
}

func validBucket(s string) bool {
	if !bucketName.MatchString(s) || strings.Contains(s, "..") || ipv4Name.MatchString(s) {
		return false
	}
	for _, prefix := range []string{"xn--", "sthree-", "amzn-s3-demo-"} {
		if strings.HasPrefix(s, prefix) {
			return false
		}
	}
	for _, suffix := range []string{"-s3alias", "--ol-s3", ".mrap", "--x-s3", "--table-s3", "-an"} {
		if strings.HasSuffix(s, suffix) {
			return false
		}
	}
	return true
}
