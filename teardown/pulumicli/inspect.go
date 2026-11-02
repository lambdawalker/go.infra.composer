package pulumicli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lambdawalker/go.infra.composer/teardown"
)

func probe(r resource, t teardown.Target) teardown.Artifact {
	a := teardown.Artifact{ID: r.ID, Kind: "unsupported", Region: t.Region, TargetID: t.ID, Data: routing(t)}
	a.Data["pulumi_type"] = r.Type
	value := func(k string) string { var s string; _ = json.Unmarshal(r.Outputs[k], &s); return s }
	parent := func(k, key string) { a.Kind = k; a.ID = value(key) }
	switch r.Type {
	case "aws:lambda/function:Function":
		a.Kind = "lambda"
	case "aws:lambda/eventSourceMapping:EventSourceMapping":
		a.Kind = "mapping"
	case "aws:lambda/permission:Permission":
		parent("lambda", "function")
	case "aws:iam/role:Role":
		a.Kind = "role"
	case "aws:iam/rolePolicy:RolePolicy", "aws:iam/rolePolicyAttachment:RolePolicyAttachment":
		parent("role", "role")
	case "aws:dynamodb/table:Table":
		a.Kind = "table"
	case "aws:s3/bucketV2:BucketV2", "aws:s3/bucket:Bucket":
		a.Kind = "bucket"
	case "aws:s3/bucketVersioningV2:BucketVersioningV2", "aws:s3/bucketLifecycleConfigurationV2:BucketLifecycleConfigurationV2", "aws:s3/bucketOwnershipControls:BucketOwnershipControls", "aws:s3/bucketPublicAccessBlock:BucketPublicAccessBlock", "aws:s3/bucketServerSideEncryptionConfigurationV2:BucketServerSideEncryptionConfigurationV2", "aws:s3/bucketPolicy:BucketPolicy", "aws:s3/bucketObjectv2:BucketObjectv2":
		parent("bucket", "bucket")
	case "aws:cloudfront/distribution:Distribution":
		a.Kind = "distribution"
	case "aws:cloudfront/originAccessControl:OriginAccessControl":
		a.Kind = "origin-access-control"
	case "aws:sqs/queue:Queue":
		a.Kind = "queue"
	case "aws:cognito/userPool:UserPool":
		a.Kind = "pool"
	case "aws:cognito/userPoolClient:UserPoolClient", "aws:cognito/managedLoginBranding:ManagedLoginBranding", "aws:cognito/userPoolDomain:UserPoolDomain":
		parent("pool", "userPoolId")
	case "aws:ses/domainIdentity:DomainIdentity":
		a.Kind = "ses"
	case "aws:ses/domainDkim:DomainDkim":
		parent("ses", "domain")
	case "aws:apigatewayv2/api:Api":
		a.Kind = "api"
	case "aws:apigatewayv2/integration:Integration", "aws:apigatewayv2/route:Route", "aws:apigatewayv2/stage:Stage", "aws:apigatewayv2/authorizer:Authorizer":
		parent("api", "apiId")
	case "aws:apigatewayv2/domainName:DomainName":
		a.Kind = "domain"
	case "aws:apigatewayv2/apiMapping:ApiMapping":
		parent("domain", "domainName")
	case "aws:acm/certificate:Certificate":
		a.Kind = "certificate"
	case "aws:acm/certificateValidation:CertificateValidation":
		parent("certificate", "certificateArn")
	case "aws:cloudwatch/eventRule:EventRule":
		a.Kind = "rule"
		if value("name") != "" {
			a.ID = value("name")
		}
	case "aws:cloudwatch/eventTarget:EventTarget":
		parent("rule", "rule")
	case "aws:cloudwatch/metricAlarm:MetricAlarm":
		a.Kind = "alarm"
	case "aws:route53/record:Record":
		a.Kind = "record"
		a.ID = value("name")
		a.Data["zone"] = value("zoneId")
		a.Data["type"] = value("type")
	}
	if r.External {
		a.Kind = "unsupported"
	}
	return a
}
func (d *Driver) artifactTarget(a teardown.Artifact) (teardown.Target, error) {
	for _, p := range d.Config.Projects {
		prefix := p.Backend + "|" + p.Name + "|"
		if strings.HasPrefix(a.TargetID, prefix) {
			return target(p, strings.TrimPrefix(a.TargetID, prefix)), nil
		}
		if strings.HasPrefix(a.ID, prefix) && a.Kind == "stack" {
			return target(p, strings.TrimPrefix(a.ID, prefix)), nil
		}
	}
	t := teardown.Target{ID: a.TargetID, Backend: a.Data["backend"], Project: a.Data["project"], Stack: a.Data["stack"], Directory: a.Data["directory"], Region: a.Region}
	if !d.configured(t) {
		return t, errors.New("artifact target is not configured")
	}
	return t, nil
}
func (d *Driver) Inspect(ctx context.Context, a teardown.Artifact) (teardown.State, error) {
	if e := d.identity(ctx); e != nil {
		return "", e
	}
	if !a.Recovery && a.TargetID != "" {
		if _, e := d.artifactTarget(a); e != nil {
			return "", e
		}
	}
	if a.Kind == "stack" {
		t, e := d.artifactTarget(a)
		if e != nil {
			return "", e
		}
		_, _, e = d.state(ctx, t)
		if errors.Is(e, teardown.ErrAbsent) {
			return teardown.StateAbsent, nil
		}
		if e != nil {
			return "", e
		}
		return teardown.StatePresent, nil
	}
	if a.ID == "" {
		return "", errors.New("missing resource identifier")
	}
	if e := d.checkARN(a.ID); e != nil {
		return "", e
	}
	if a.Kind == "aws_backup_collection" {
		b, e := d.aws(ctx, a.Region, "backup", "list-recovery-points-by-resource", "--resource-arn", a.ID)
		if e != nil {
			return "", e
		}
		var response struct{ RecoveryPoints *[]json.RawMessage }
		if json.Unmarshal(b, &response) != nil || response.RecoveryPoints == nil {
			return "", errors.New("incomplete AWS Backup recovery point listing")
		}
		if len(*response.RecoveryPoints) > 0 {
			return teardown.StatePresent, nil
		}
		return teardown.StateAbsent, nil
	}
	if a.Kind == "table_backups" {
		backups, e := d.backups(ctx, a.Region, a.ID)
		if e != nil {
			return "", e
		}
		if len(backups) > 0 {
			return teardown.StatePresent, nil
		}
		return teardown.StateAbsent, nil
	}
	args := []string{}
	notFound := "ResourceNotFoundException"
	field := ""
	switch a.Kind {
	case "lambda":
		args = []string{"lambda", "get-function-configuration", "--function-name", a.ID}
	case "mapping":
		args = []string{"lambda", "get-event-source-mapping", "--uuid", a.ID}
	case "role":
		args = []string{"iam", "get-role", "--role-name", a.ID}
		notFound = "NoSuchEntity"
	case "table":
		args = []string{"dynamodb", "describe-table", "--table-name", a.ID}
	case "bucket", "recovery_bucket":
		args = []string{"s3api", "get-bucket-location", "--bucket", a.ID, "--expected-bucket-owner", d.Config.Scope.Account}
		notFound = "NoSuchBucket"
	case "queue":
		args = []string{"sqs", "get-queue-attributes", "--queue-url", a.ID, "--attribute-names", "QueueArn"}
		notFound = "AWS.SimpleQueueService.NonExistentQueue"
	case "pool":
		args = []string{"cognito-idp", "describe-user-pool", "--user-pool-id", a.ID}
	case "ses":
		args = []string{"ses", "get-identity-verification-attributes", "--identities", a.ID}
		field = "VerificationAttributes"
	case "api":
		args = []string{"apigatewayv2", "get-api", "--api-id", a.ID}
		notFound = "NotFoundException"
	case "domain":
		args = []string{"apigatewayv2", "get-domain-name", "--domain-name", a.ID}
		notFound = "NotFoundException"
	case "distribution":
		args = []string{"cloudfront", "get-distribution", "--id", a.ID}
		notFound = "NoSuchDistribution"
	case "origin-access-control":
		args = []string{"cloudfront", "get-origin-access-control", "--id", a.ID}
		notFound = "NoSuchOriginAccessControl"
	case "certificate":
		args = []string{"acm", "describe-certificate", "--certificate-arn", a.ID}
		p := strings.Split(a.ID, ":")
		if len(p) == 6 && p[3] != "" {
			a.Region = p[3]
		}
	case "rule":
		args = []string{"events", "describe-rule", "--name", a.ID}
	case "alarm":
		args = []string{"cloudwatch", "describe-alarms", "--alarm-names", a.ID, "--alarm-types", "MetricAlarm", "CompositeAlarm"}
		field = "MetricAlarms"
	case "record":
		if a.Data["zone"] == "" || a.Data["type"] == "" {
			return "", errors.New("DNS identifier missing")
		}
		input, _ := json.Marshal(map[string]string{"HostedZoneId": a.Data["zone"], "StartRecordName": a.ID, "StartRecordType": a.Data["type"], "MaxItems": "1"})
		args = []string{"route53", "list-resource-record-sets", "--cli-input-json", string(input), "--no-paginate"}
		field = "ResourceRecordSets"
		notFound = "NoSuchHostedZone"
	case "log_group":
		args = []string{"logs", "describe-log-groups", "--log-group-name-prefix", a.ID}
		field = "logGroups"
	case "dynamodb_backup", "aws_backup":
		args = []string{"dynamodb", "describe-backup", "--backup-arn", a.ID}
		field = "BackupDescription"
		notFound = "BackupNotFoundException"
	case "recovery_key":
		args = []string{"kms", "describe-key", "--key-id", a.ID}
		notFound = "NotFoundException"
		field = "KeyMetadata"
	default:
		return "", errors.New("unsupported resource type")
	}
	b, e := d.aws(ctx, a.Region, args...)
	if e != nil {
		if missing(e, notFound) {
			return teardown.StateAbsent, nil
		}
		return "", e
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil || len(obj) == 0 {
		return "", errors.New("incomplete AWS verification response")
	}
	if field == "" {
		return teardown.StatePresent, nil
	}
	raw, ok := obj[field]
	if !ok || string(raw) == "null" {
		return "", errors.New("incomplete AWS verification response")
	}
	switch a.Kind {
	case "log_group":
		var groups []struct{ LogGroupName string }
		if json.Unmarshal(raw, &groups) != nil || groups == nil {
			return "", errors.New("invalid log group response")
		}
		for _, g := range groups {
			if g.LogGroupName == "" {
				return "", errors.New("incomplete log group response")
			}
			if g.LogGroupName == a.ID {
				return teardown.StatePresent, nil
			}
		}
	case "dynamodb_backup", "aws_backup":
		var desc struct{ BackupDetails struct{ BackupStatus string } }
		if json.Unmarshal(raw, &desc) != nil || desc.BackupDetails.BackupStatus == "" {
			return "", errors.New("incomplete backup response")
		}
		if desc.BackupDetails.BackupStatus == "DELETED" {
			return teardown.StateAbsent, nil
		}
		if desc.BackupDetails.BackupStatus == "DELETING" {
			return teardown.StatePending, nil
		}
		return teardown.StatePresent, nil
	case "recovery_key":
		var meta struct{ KeyState string }
		if json.Unmarshal(raw, &meta) != nil || meta.KeyState == "" {
			return "", errors.New("invalid KMS metadata")
		}
		if meta.KeyState == "PendingDeletion" || meta.KeyState == "PendingReplicaDeletion" {
			return teardown.StatePending, nil
		}
		return teardown.StatePresent, nil
	case "ses":
		var ids map[string]json.RawMessage
		if json.Unmarshal(raw, &ids) != nil {
			return "", errors.New("invalid SES response")
		}
		if len(ids) > 0 {
			return teardown.StatePresent, nil
		}
	case "record":
		var records []struct{ Name, Type string }
		if json.Unmarshal(raw, &records) != nil || records == nil {
			return "", errors.New("invalid DNS response")
		}
		for _, r := range records {
			if r.Name == "" || r.Type == "" {
				return "", errors.New("incomplete DNS record")
			}
			if strings.TrimSuffix(strings.ToLower(r.Name), ".") == strings.TrimSuffix(strings.ToLower(a.ID), ".") && r.Type == a.Data["type"] {
				return teardown.StatePresent, nil
			}
		}
	case "alarm":
		for _, k := range []string{"MetricAlarms", "CompositeAlarms"} {
			var alarms []json.RawMessage
			if json.Unmarshal(obj[k], &alarms) != nil || alarms == nil {
				return "", errors.New("incomplete alarm response")
			}
			if len(alarms) > 0 {
				return teardown.StatePresent, nil
			}
		}
	}
	return teardown.StateAbsent, nil
}
