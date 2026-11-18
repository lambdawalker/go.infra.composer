package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type bucketReceipt struct {
	Version               int
	Account, Region, Name string
	Created, Ready        bool
}

// bootstrapBucket owns only a newly generated bucket. Receipts allow configuration recovery.
func bootstrapBucket(ctx context.Context, private, account, region, bucketPrefix string) (string, error) {
	return bootstrapWith(ctx, private, account, region, bucketPrefix, func(args ...string) error { return command(ctx, "", append([]string{"aws", "s3api"}, args...)...) })
}
func bootstrapWith(ctx context.Context, private, account, region, bucketPrefix string, run func(...string) error) (string, error) {
	path := filepath.Join(private, "state-bucket.json")
	r := bucketReceipt{Version: 1, Account: account, Region: region}
	b, e := os.ReadFile(path)
	if e == nil {
		if json.Unmarshal(b, &r) != nil || r.Version > 1 || r.Account != account || r.Region != region {
			return "", fmt.Errorf("state bucket receipt scope mismatch; preserve receipt and explicitly select an existing bucket")
		}
	} else if !os.IsNotExist(e) {
		return "", e
	} else {
		id := make([]byte, 6)
		if _, e = rand.Read(id); e != nil {
			return "", e
		}
		r.Name = bucketPrefix + "-" + account + "-" + hex.EncodeToString(id)
		if e = saveBucketReceipt(path, r); e != nil {
			return "", e
		}
	}
	r.Version = 1
	fmt.Printf("State bucket: %s • account %s • region %s\n", r.Name, account, region)
	if !r.Created && len(b) > 0 {
		// A crash can occur after AWS creates the bucket but before the receipt write.
		e = run("head-bucket", "--bucket", r.Name, "--expected-bucket-owner", account)
		if e == nil {
			r.Created = true
			if e = saveBucketReceipt(path, r); e != nil {
				return "", e
			}
		} else if !strings.Contains(e.Error(), "404") && !strings.Contains(e.Error(), "NoSuchBucket") {
			return "", fmt.Errorf("pending bootstrap ownership is unknown; recover this exact bucket: %w", e)
		}
	}
	if !r.Created {
		args := []string{"create-bucket", "--bucket", r.Name, "--region", region, "--object-ownership", "BucketOwnerEnforced"}
		if region != "us-east-1" {
			args = append(args, "--create-bucket-configuration", "LocationConstraint="+region)
		}
		if e = run(args...); e != nil {
			return "", fmt.Errorf("bucket creation failed; receipt preserved, no alternate bucket created: %w", e)
		}
		r.Created = true
		if e = saveBucketReceipt(path, r); e != nil {
			return "", e
		}
	}
	if e = run("head-bucket", "--bucket", r.Name, "--expected-bucket-owner", account); e != nil {
		return "", e
	}
	// Repeat all managed settings after a partial failure; never declare readiness early.
	for _, args := range [][]string{
		{"put-public-access-block", "--public-access-block-configuration", `{"BlockPublicAcls":true,"IgnorePublicAcls":true,"BlockPublicPolicy":true,"RestrictPublicBuckets":true}`},
		{"put-bucket-versioning", "--versioning-configuration", `{"Status":"Enabled"}`},
		{"put-bucket-encryption", "--server-side-encryption-configuration", `{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}`},
		{"put-bucket-policy", "--policy", fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"s3:*","Resource":["arn:aws:s3:::%s","arn:aws:s3:::%s/*"],"Condition":{"Bool":{"aws:SecureTransport":"false"}}}]}`, r.Name, r.Name)},
	} {
		args = append(args, "--bucket", r.Name, "--expected-bucket-owner", account)
		if e = run(args...); e != nil {
			return "", fmt.Errorf("state bucket bootstrap incomplete; rerun the same environment to resume: %w", e)
		}
	}
	r.Ready = true
	return r.Name, saveBucketReceipt(path, r)
}
func saveBucketReceipt(path string, r bucketReceipt) error {
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(path, b)
}
