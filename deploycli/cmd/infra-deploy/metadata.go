package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

func tagManagedStateBucket(ctx context.Context, bucket, account, project, environment string) error {
	cmd := exec.CommandContext(ctx, "aws", "s3api", "get-bucket-tagging", "--bucket", bucket, "--expected-bucket-owner", account, "--output", "json")
	b, e := cmd.CombinedOutput()
	var existing struct{ TagSet []struct{ Key, Value string } }
	if e != nil && !strings.Contains(string(b), "(NoSuchTagSet)") {
		return fmt.Errorf("cannot inspect state bucket tags: %w", e)
	}
	if e == nil && json.Unmarshal(b, &existing) != nil {
		return fmt.Errorf("invalid state bucket tags")
	}
	tags := map[string]string{}
	for _, t := range existing.TagSet {
		tags[t.Key] = t.Value
	}
	if !mergeStateTags(tags, project, environment) {
		return nil
	}

	payload := struct{ TagSet []map[string]string }{TagSet: []map[string]string{}}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		payload.TagSet = append(payload.TagSet, map[string]string{"Key": k, "Value": tags[k]})
	}
	encoded, _ := json.Marshal(payload)
	return command(ctx, "", "aws", "s3api", "put-bucket-tagging", "--bucket", bucket, "--expected-bucket-owner", account, "--tagging", string(encoded))
}

func mergeStateTags(tags map[string]string, project, environment string) bool {
	identity := project + "-" + environment
	changed := false
	for k, v := range map[string]string{"Name": identity, "Project": project, "Environment": environment, "ManagedBy": "infra-composer", "Component": "deployment-state", "Description": identity + ": Pulumi deployment state"} {
		if tags[k] != v {
			tags[k] = v
			changed = true
		}
	}
	return changed
}
