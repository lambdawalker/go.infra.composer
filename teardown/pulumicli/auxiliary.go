package pulumicli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lambdawalker/go.infra.composer/teardown"
)

// auxiliary snapshots identifiers before mutation, so cleanup remains retryable
// after Pulumi has already removed the stack or its parent resources.
func (d *Driver) auxiliary(ctx context.Context, t teardown.Target, r resource) ([]teardown.Artifact, error) {
	if r.External {
		return nil, nil
	}
	a := probe(r, t)
	switch r.Type {
	case "aws:lambda/function:Function":
		name := a.ID
		if strings.HasPrefix(name, "arn:") {
			parts := strings.Split(name, ":")
			if len(parts) < 7 || parts[5] != "function" {
				return nil, errors.New("invalid Lambda identifier")
			}
			name = parts[6]
		}
		if name == "" || strings.Contains(name, "/") {
			return nil, errors.New("invalid Lambda name")
		}
		data := routing(t)
		data["cleanup"] = "true"
		data["parent_kind"] = "lambda"
		data["parent_id"] = a.ID
		return []teardown.Artifact{{ID: "/aws/lambda/" + name, Kind: "log_group", Region: t.Region, TargetID: t.ID, Data: data}}, nil
	case "aws:dynamodb/table:Table":
		if a.ID == "" {
			return nil, errors.New("missing table name")
		}
		backups, e := d.backups(ctx, t.Region, a.ID)
		if e != nil {
			return nil, e
		}
		collectionData := routing(t)
		collectionData["cleanup"] = "true"
		collectionData["parent_kind"] = "table"
		collectionData["parent_id"] = a.ID
		result := []teardown.Artifact{{ID: a.ID, Kind: "table_backups", Region: t.Region, TargetID: t.ID, Data: collectionData}}
		result = append(result, teardown.Artifact{ID: d.tableARN(t.Region, a.ID), Kind: "aws_backup_collection", Region: t.Region, TargetID: t.ID, Data: collectionData})
		for _, backup := range backups {
			if backup.TableName != a.ID || !d.backupOwned(backup.BackupArn, a.ID, t.Region) {
				return nil, errors.New("backup ownership mismatch")
			}
			kind := "dynamodb_backup"
			switch backup.BackupType {
			case "AWS_BACKUP":
				kind = "aws_backup"
			case "USER", "SYSTEM":
			default:
				return nil, errors.New("unrecognized backup type")
			}
			data := routing(t)
			data["cleanup"] = "true"
			data["parent_kind"] = "table"
			data["parent_id"] = a.ID
			data["backup_type"] = backup.BackupType
			result = append(result, teardown.Artifact{ID: backup.BackupArn, Kind: kind, Region: t.Region, TargetID: t.ID, Data: data})
		}
		return result, nil
	}
	return nil, nil
}
func (d *Driver) backupOwned(arn, table, region string) bool {
	p := strings.SplitN(arn, ":", 6)
	return len(p) == 6 && p[0] == "arn" && p[2] == "dynamodb" && p[3] == region && p[4] == d.Config.Scope.Account && strings.HasPrefix(p[5], "table/"+table+"/backup/") && len(p[5]) > len("table/"+table+"/backup/")
}

// Cleanup removes only persisted auxiliary resources after authoritative parent
// absence. AWS Backup recovery points require their own service workflow.
func (d *Driver) Cleanup(ctx context.Context, a teardown.Artifact) error {
	if e := d.identity(ctx); e != nil {
		return e
	}
	if a.Data["cleanup"] != "true" {
		return errors.New("artifact is not authorized for auxiliary cleanup")
	}
	if _, e := d.artifactTarget(a); e != nil {
		return e
	}
	parent := teardown.Artifact{ID: a.Data["parent_id"], Kind: a.Data["parent_kind"], Region: a.Region, TargetID: a.TargetID}
	if parent.ID == "" {
		return errors.New("auxiliary parent missing")
	}
	switch a.Kind {
	case "log_group":
		name := parent.ID
		if strings.HasPrefix(name, "arn:") {
			parts := strings.Split(name, ":")
			if len(parts) < 7 {
				return errors.New("invalid Lambda parent")
			}
			name = parts[6]
		}
		if parent.Kind != "lambda" || a.ID != "/aws/lambda/"+name {
			return errors.New("log group ownership mismatch")
		}
	case "aws_backup_collection":
		if parent.Kind != "table" || a.ID != d.tableARN(a.Region, parent.ID) {
			return errors.New("AWS Backup collection ownership mismatch")
		}
	case "table_backups":
		if parent.Kind != "table" || a.ID != parent.ID {
			return errors.New("backup collection ownership mismatch")
		}
	case "dynamodb_backup":
		if parent.Kind != "table" || !d.backupOwned(a.ID, parent.ID, a.Region) {
			return errors.New("backup ownership mismatch")
		}
	case "aws_backup":
		return errors.New("AWS Backup recovery point requires manual service cleanup")
	default:
		return errors.New("unsupported auxiliary cleanup")
	}
	state, e := d.Inspect(ctx, parent)
	if e != nil {
		return e
	}
	if state != teardown.StateAbsent {
		return errors.New("auxiliary parent remains")
	}
	if a.Kind == "table_backups" {
		backups, e := d.backups(ctx, a.Region, a.ID)
		if e != nil {
			return e
		}
		var failures []error
		for _, b := range backups {
			if b.BackupType != "USER" {
				failures = append(failures, errors.New("SYSTEM or AWS Backup recovery point retained for service cleanup"))
				continue
			}
			_, e = d.aws(ctx, a.Region, "dynamodb", "delete-backup", "--backup-arn", b.BackupArn)
			if e != nil && !missing(e, "BackupNotFoundException") {
				failures = append(failures, e)
			}
		}
		return errors.Join(failures...)
	}
	if a.Kind == "dynamodb_backup" && a.Data["backup_type"] != "USER" {
		return errors.New("non-user backup retained for service cleanup")
	}
	state, e = d.Inspect(ctx, a)
	if e != nil {
		return e
	}
	if state == teardown.StateAbsent || state == teardown.StatePending {
		return nil
	}
	switch a.Kind {
	case "aws_backup_collection":
		return errors.New("AWS Backup recovery points retained for manual service cleanup")
	case "log_group":
		_, e = d.aws(ctx, a.Region, "logs", "delete-log-group", "--log-group-name", a.ID)
		if missing(e, "ResourceNotFoundException") {
			return nil
		}
	case "dynamodb_backup":
		_, e = d.aws(ctx, a.Region, "dynamodb", "delete-backup", "--backup-arn", a.ID)
		if missing(e, "BackupNotFoundException") {
			return nil
		}
	}
	return e
}

type backupSummary struct{ BackupArn, BackupType, BackupStatus, TableName string }

func (d *Driver) backups(ctx context.Context, region, table string) ([]backupSummary, error) {
	b, e := d.aws(ctx, region, "dynamodb", "list-backups", "--table-name", table, "--backup-type", "ALL")
	if e != nil {
		return nil, e
	}
	var response struct{ BackupSummaries *[]backupSummary }
	if json.Unmarshal(b, &response) != nil || response.BackupSummaries == nil {
		return nil, errors.New("incomplete backup inventory")
	}
	for _, backup := range *response.BackupSummaries {
		if backup.TableName != table || !d.backupOwned(backup.BackupArn, table, region) {
			return nil, errors.New("backup ownership mismatch")
		}
		switch backup.BackupType {
		case "USER", "SYSTEM", "AWS_BACKUP":
		default:
			return nil, errors.New("unrecognized backup type")
		}
	}
	return *response.BackupSummaries, nil
}

func (d *Driver) tableARN(region, table string) string {
	partition := "aws"
	switch {
	case strings.HasPrefix(region, "cn-"):
		partition = "aws-cn"
	case strings.HasPrefix(region, "us-gov-"):
		partition = "aws-us-gov"
	case strings.HasPrefix(region, "us-isob-"):
		partition = "aws-iso-b"
	case strings.HasPrefix(region, "us-isof-"):
		partition = "aws-iso-f"
	case strings.HasPrefix(region, "eu-isoe-"):
		partition = "aws-iso-e"
	case strings.HasPrefix(region, "us-iso-"):
		partition = "aws-iso"
	}
	return "arn:" + partition + ":dynamodb:" + region + ":" + d.Config.Scope.Account + ":table/" + table
}
