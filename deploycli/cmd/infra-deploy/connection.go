package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type awsIdentity struct{ Account, Arn string }

func connectionEnv(profile, region string, ci bool) (func(), error) {
	var restore []func()
	cleanup := func() {
		for i := len(restore) - 1; i >= 0; i-- {
			restore[i]()
		}
	}
	settings := [][2]string{{"AWS_REGION", region}, {"AWS_DEFAULT_REGION", region}}
	if !ci {
		settings = append(settings, [2]string{"AWS_PROFILE", profile})
	}
	for _, setting := range settings {
		reset, err := setRunEnv(setting[0], setting[1])
		if err != nil {
			cleanup()
			return nil, err
		}
		restore = append(restore, reset)
	}
	return cleanup, nil
}

// checkConnection verifies the editable target before setup can mutate it.
// Existing state buckets may be checked read-only during browser preflight.
func checkConnection(ctx context.Context, root string, target Settings, ci bool, workers int, checkBucket bool) (awsIdentity, string, error) {
	ui := wizard(ctx)
	ui.phase("connect", "Verify cloud access")
	var identity awsIdentity
	for _, tool := range []string{"aws", "pulumi", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			return identity, "", fmt.Errorf("install %s and add it to PATH", tool)
		}
	}
	restore, err := connectionEnv(target.Profile, target.Region, ci)
	if err != nil {
		return identity, "", err
	}
	defer restore()
	if !ci {
		if err := verifySSOProfile(ctx, target.Profile); err != nil {
			return identity, "profile", err
		}
	}
	if err = prerequisites(ctx, workers, []func() error{
		func() error {
			b, err := output(ctx, "aws", "--version")
			if err != nil || !strings.Contains(string(b), "aws-cli/2.") {
				return errors.New("AWS CLI v2 required")
			}
			return nil
		},
		func() error {
			b, err := output(ctx, "pulumi", "version")
			if err != nil || !strings.HasPrefix(strings.TrimSpace(string(b)), "v3.") {
				return errors.New("Pulumi CLI v3 required")
			}
			return nil
		},
	}); err != nil {
		return identity, "", err
	}
	raw, err := authenticatedIdentity(ci, target.Profile, func() ([]byte, error) {
		return output(ctx, "aws", "sts", "get-caller-identity", "--output", "json")
	}, func() error {
		args := []string{"aws", "sso", "login", "--profile", target.Profile}
		if ui.browser != nil {
			args = append(args, "--use-device-code", "--no-browser")
		}
		return ui.login(root, "AWS", args...)
	})
	if err != nil {
		return identity, "profile", fmt.Errorf("AWS sign-in did not complete for profile %q: %w", target.Profile, err)
	}
	if json.Unmarshal(raw, &identity) != nil || !pattern(`^\d{12}$`, identity.Account) {
		return identity, "profile", errors.New("invalid AWS identity")
	}
	if ci && !strings.Contains(identity.Arn, ":assumed-role/") {
		return identity, "profile", errors.New("CI requires an assumed role")
	}
	if !ci && !strings.Contains(identity.Arn, ":assumed-role/AWSReservedSSO_") {
		return identity, "profile", errors.New("interactive deployments require an IAM Identity Center SSO profile")
	}
	if target.Account != "" && target.Account != identity.Account {
		return identity, "account", errors.New("AWS account differs from the selected target; choose a matching SSO profile or separate deployment scope")
	}
	if checkBucket {
		if err := verifyStateBucket(ctx, root, target.Backend, identity.Account, target.Region); err != nil {
			return identity, "backend", err
		}
	}
	return identity, "", nil
}

func verifyStateBucket(ctx context.Context, root, backend, account, region string) error {
	bucket, statePrefix, ok := strings.Cut(strings.TrimPrefix(backend, "s3://"), "/")
	if !ok {
		return errors.New("state bucket must include an S3 prefix")
	}
	if e := command(ctx, root, "aws", "s3api", "head-bucket", "--bucket", bucket, "--expected-bucket-owner", account); e != nil {
		return fmt.Errorf("cannot access the S3 state bucket with this AWS profile: %w", e)
	}
	for _, check := range []struct{ cmd, field, want string }{{"get-bucket-versioning", "Status", "Enabled"}, {"get-bucket-encryption", "ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm", ""}, {"get-public-access-block", "PublicAccessBlockConfiguration", ""}, {"get-bucket-location", "LocationConstraint", ""}} {
		b, e := output(ctx, "aws", "s3api", check.cmd, "--bucket", bucket, "--expected-bucket-owner", account, "--query", check.field, "--output", "json")
		if e != nil {
			return fmt.Errorf("cannot verify the S3 state bucket settings (%s): %w", check.cmd, e)
		}
		if check.cmd == "get-bucket-versioning" && strings.TrimSpace(string(b)) != `"Enabled"` {
			return errors.New("state bucket must have versioning enabled")
		}
		if check.cmd == "get-bucket-encryption" && !strings.Contains(string(b), "AES256") && !strings.Contains(string(b), "aws:kms") {
			return errors.New("state bucket encryption unavailable")
		}
		if check.cmd == "get-public-access-block" {
			var fields map[string]bool
			if json.Unmarshal(b, &fields) != nil || !fields["BlockPublicAcls"] || !fields["BlockPublicPolicy"] || !fields["IgnorePublicAcls"] || !fields["RestrictPublicBuckets"] {
				return errors.New("state bucket must block all public access")
			}
		}
		if check.cmd == "get-bucket-location" {
			loc := strings.Trim(strings.TrimSpace(string(b)), `"`)
			if loc == "null" {
				loc = "us-east-1"
			}
			if loc != region {
				return errors.New("state bucket region differs from selected region")
			}
		}
	}
	policyBytes, e := output(ctx, "aws", "s3api", "get-bucket-policy", "--bucket", bucket, "--expected-bucket-owner", account, "--output", "json")
	if e != nil {
		return fmt.Errorf("cannot read the S3 state bucket policy: %w", e)
	}
	if e := requireStateTLS(policyBytes, bucket); e != nil {
		return e
	}
	if _, e = output(ctx, "aws", "s3api", "list-objects-v2", "--bucket", bucket, "--prefix", statePrefix+"/", "--max-keys", "1", "--expected-bucket-owner", account, "--output", "json"); e != nil {
		return fmt.Errorf("state prefix is inaccessible: %w", e)
	}
	return nil
}
