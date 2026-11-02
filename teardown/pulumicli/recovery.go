package pulumicli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lambdawalker/go.infra.composer/teardown"
)

type version struct{ Key, VersionId string }

func (d *Driver) versions(ctx context.Context, a teardown.Artifact) ([]version, error) {
	var all []version
	keyMarker, versionMarker := "", ""
	seen := map[string]bool{}
	for {
		args := []string{"s3api", "list-object-versions", "--bucket", a.ID, "--expected-bucket-owner", d.Config.Scope.Account, "--no-paginate"}
		if keyMarker != "" {
			args = append(args, "--key-marker", keyMarker)
			if versionMarker != "" {
				args = append(args, "--version-id-marker", versionMarker)
			}
		}
		b, e := d.aws(ctx, a.Region, args...)
		if missing(e, "NoSuchBucket") {
			return nil, teardown.ErrAbsent
		}
		if e != nil {
			return nil, e
		}
		var v struct {
			Versions, DeleteMarkers            []version
			IsTruncated                        bool
			NextKeyMarker, NextVersionIdMarker string
		}
		if json.Unmarshal(b, &v) != nil {
			return nil, errors.New("invalid bucket versions response")
		}
		page := append(v.Versions, v.DeleteMarkers...)
		for _, o := range page {
			if o.Key == "" || o.VersionId == "" {
				return nil, errors.New("incomplete object version")
			}
		}
		all = append(all, page...)
		if !v.IsTruncated {
			return all, nil
		}
		marker := v.NextKeyMarker + "\x00" + v.NextVersionIdMarker
		if v.NextKeyMarker == "" || seen[marker] {
			return nil, errors.New("incomplete bucket pagination")
		}
		seen[marker] = true
		keyMarker, versionMarker = v.NextKeyMarker, v.NextVersionIdMarker
	}
}

// Delete requests are bounded to the S3 maximum of 1000 versions; per-object
// errors are checked even when the HTTP request succeeded.
func (d *Driver) purge(ctx context.Context, a teardown.Artifact) error {
	objects, e := d.versions(ctx, a)
	if errors.Is(e, teardown.ErrAbsent) {
		return nil
	}
	if e != nil {
		return e
	}
	for len(objects) > 0 {
		n := min(1000, len(objects))
		payload, _ := json.Marshal(struct {
			Objects []version
			Quiet   bool
		}{objects[:n], true})
		b, e := d.aws(ctx, a.Region, "s3api", "delete-objects", "--bucket", a.ID, "--expected-bucket-owner", d.Config.Scope.Account, "--delete", string(payload))
		if e != nil {
			return e
		}
		var result struct{ Errors []json.RawMessage }
		if json.Unmarshal(b, &result) != nil {
			return errors.New("invalid object deletion response")
		}
		if len(result.Errors) > 0 {
			return errors.New("bucket object deletion failed")
		}
		objects = objects[n:]
	}
	return nil
}
func (d *Driver) recoveryConfigured(a teardown.Artifact) bool {
	for _, v := range d.Config.Recovery {
		if v.ID == a.ID && v.Kind == a.Kind && v.Region == a.Region && v.Recovery && v.Data["exclusive"] == "true" && a.Data["exclusive"] == "true" {
			return true
		}
	}
	return false
}
func (d *Driver) recognizedKey(key string) bool {
	if key == ".pulumi/meta.yaml" {
		return true
	}
	p := strings.Split(key, "/")
	if len(p) < 4 || p[0] != ".pulumi" {
		return false
	}
	switch p[1] {
	case "stacks", "history", "backups", "locks":
	default:
		return false
	}
	projectIndex := 2
	if p[2] == "organization" {
		if len(p) < 5 {
			return false
		}
		projectIndex = 3
	}
	for _, project := range d.Config.Projects {
		if p[projectIndex] == project.Name {
			return true
		}
	}
	return false
}
func (d *Driver) keySafe(ctx context.Context, a teardown.Artifact) error {
	b, e := d.aws(ctx, a.Region, "kms", "describe-key", "--key-id", a.ID)
	if missing(e, "NotFoundException") {
		return teardown.ErrAbsent
	}
	if e != nil {
		return e
	}
	var v struct {
		KeyMetadata struct {
			AWSAccountId, KeyManager, KeyState, Arn string
			MultiRegion                             bool
			MultiRegionConfiguration                struct {
				MultiRegionKeyType string
				ReplicaKeys        []json.RawMessage
			}
		}
	}
	if json.Unmarshal(b, &v) != nil {
		return errors.New("invalid KMS response")
	}
	m := v.KeyMetadata
	if m.AWSAccountId != d.Config.Scope.Account || m.KeyManager != "CUSTOMER" {
		return errors.New("KMS key is not customer-managed in scoped account")
	}
	if m.MultiRegion && (m.MultiRegionConfiguration.MultiRegionKeyType != "PRIMARY" || len(m.MultiRegionConfiguration.ReplicaKeys) > 0) {
		return errors.New("KMS multi-region key cannot be safely scheduled")
	}
	if e = d.checkARN(m.Arn); e != nil {
		return e
	}
	b, e = d.aws(ctx, a.Region, "kms", "list-resource-tags", "--key-id", a.ID)
	if e != nil {
		return e
	}
	var tags struct {
		Tags []struct{ TagKey, TagValue string }
	}
	if json.Unmarshal(b, &tags) != nil {
		return errors.New("invalid KMS tags")
	}
	for _, t := range tags.Tags {
		if t.TagKey == "ic:application" && t.TagValue != d.Config.Scope.Application {
			return errors.New("foreign KMS application tag")
		}
	}
	return nil
}
func (d *Driver) PrepareRecovery(ctx context.Context, as []teardown.Artifact) error {
	d.recoveryPrepared = false
	if e := d.identity(ctx); e != nil {
		return e
	}
	for _, p := range d.Config.Projects {
		names, e := d.list(ctx, target(p, ""), true)
		if e != nil {
			return e
		}
		if len(names) > 0 {
			return errors.New("backend still contains stacks; recovery retained")
		}
	}
	for _, a := range as {
		if !d.recoveryConfigured(a) {
			return errors.New("recovery artifact requires configured exclusive ownership")
		}
		switch a.Kind {
		case "recovery_bucket":
			backend := false
			for _, p := range d.Config.Projects {
				if p.Backend == "s3://"+a.ID {
					backend = true
				}
			}
			if !backend {
				return errors.New("recovery bucket must be a whole configured backend")
			}
			objects, e := d.versions(ctx, a)
			if errors.Is(e, teardown.ErrAbsent) {
				continue
			}
			if e != nil {
				return e
			}
			for _, o := range objects {
				if !d.recognizedKey(o.Key) {
					return errors.New("unrecognized recovery object; retained")
				}
			}
		case "recovery_key":
			if e := d.keySafe(ctx, a); e != nil && !errors.Is(e, teardown.ErrAbsent) {
				return e
			}
		default:
			return errors.New("unsupported recovery artifact")
		}
	}
	d.recoveryPrepared = true
	return nil
}
func (d *Driver) RemoveRecovery(ctx context.Context, a teardown.Artifact) error {
	if !d.recoveryPrepared || !d.recoveryConfigured(a) {
		return errors.New("recovery preflight required")
	}
	if e := d.identity(ctx); e != nil {
		return e
	}
	switch a.Kind {
	case "recovery_bucket":
		objects, e := d.versions(ctx, a)
		if errors.Is(e, teardown.ErrAbsent) {
			return nil
		}
		if e != nil {
			return e
		}
		for _, o := range objects {
			if !d.recognizedKey(o.Key) {
				return errors.New("unrecognized recovery object; retained")
			}
		}
		if e = d.purge(ctx, a); e != nil {
			return e
		}
		_, e = d.aws(ctx, a.Region, "s3api", "delete-bucket", "--bucket", a.ID, "--expected-bucket-owner", d.Config.Scope.Account)
		if missing(e, "NoSuchBucket") {
			return nil
		}
		return e
	case "recovery_key": // Recheck buckets here, too: direct users of the adapter must not bypass ordering.
		for _, b := range d.Config.Recovery {
			if b.Kind == "recovery_bucket" {
				s, e := d.Inspect(ctx, b)
				if e != nil {
					return e
				}
				if s != teardown.StateAbsent {
					return errors.New("recovery bucket remains")
				}
			}
		}
		if e := d.keySafe(ctx, a); errors.Is(e, teardown.ErrAbsent) {
			return nil
		} else if e != nil {
			return e
		}
		s, e := d.Inspect(ctx, a)
		if e != nil {
			return e
		}
		if s == teardown.StatePending || s == teardown.StateAbsent {
			return nil
		}
		_, e = d.aws(ctx, a.Region, "kms", "schedule-key-deletion", "--key-id", a.ID, "--pending-window-in-days", "7")
		return e
	}
	return errors.New("unsupported recovery artifact")
}
