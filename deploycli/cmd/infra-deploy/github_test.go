package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeGitHub struct {
	env, role, provider, secret bool
	variables                   map[string]string
	trust, policy               any
	writes, deliveries          int
	fail                        string
}

func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }
func (f *fakeGitHub) run(stdin string, args ...string) ([]byte, error) {
	call := strings.Join(args, " ")
	if f.fail != "" && strings.Contains(call, f.fail) {
		return nil, fmt.Errorf("permission denied HTTP 403")
	}
	if args[0] == "aws" {
		switch args[2] {
		case "get-open-id-connect-provider":
			if !f.provider {
				return nil, fmt.Errorf("NoSuchEntity")
			}
			return jsonBytes(map[string]any{"Url": "token.actions.githubusercontent.com", "ClientIDList": []string{"sts.amazonaws.com"}}), nil
		case "create-open-id-connect-provider":
			f.provider = true
			f.writes++
		case "get-role":
			if !f.role {
				return nil, fmt.Errorf("NoSuchEntity")
			}
			return jsonBytes(map[string]any{"Role": map[string]any{"Arn": "arn:aws:iam::123456789012:role/test-dev-deploy", "AssumeRolePolicyDocument": f.trust}}), nil
		case "create-role":
			f.role = true
			f.writes++
			for i, a := range args {
				if a == "--assume-role-policy-document" {
					json.Unmarshal([]byte(args[i+1]), &f.trust)
				}
			}
		case "get-role-policy":
			if f.policy == nil {
				return nil, fmt.Errorf("NoSuchEntity")
			}
			return jsonBytes(map[string]any{"PolicyDocument": f.policy}), nil
		case "put-role-policy":
			f.writes++
			for i, a := range args {
				if a == "--policy-document" {
					json.Unmarshal([]byte(args[i+1]), &f.policy)
				}
			}
		default:
			return nil, fmt.Errorf("unexpected AWS command %s", call)
		}
		return []byte(`{}`), nil
	}
	if strings.Contains(call, "repo view") {
		return []byte(`{"nameWithOwner":"lambdawalker/go.appexfission.aws.card-capture","defaultBranchRef":{"name":"main"}}`), nil
	}
	if call == "gh api user" {
		return []byte(`{"login":"operator","id":1}`), nil
	}
	if strings.Contains(call, "secret set") {
		if stdin == "" {
			return nil, fmt.Errorf("secret stdin empty")
		}
		f.secret = true
		f.writes++
		f.deliveries++
		return nil, nil
	}
	if strings.Contains(call, "--method") {
		f.writes++
		if strings.Contains(call, "/variables") {
			var v struct{ Name, Value string }
			json.Unmarshal([]byte(stdin), &v)
			f.variables[v.Name] = v.Value
		} else {
			f.env = true
		}
		return []byte(`{}`), nil
	}
	if strings.Contains(call, "/variables?") {
		var vars []map[string]string
		for k, v := range f.variables {
			vars = append(vars, map[string]string{"name": k, "value": v})
		}
		return jsonBytes([]any{map[string]any{"variables": vars}}), nil
	}
	if strings.Contains(call, "/secrets?") {
		names := []any{}
		if f.secret {
			names = append(names, map[string]string{"name": "PULUMI_CONFIG_PASSPHRASE"})
		}
		return jsonBytes([]any{map[string]any{"secrets": names}}), nil
	}
	if strings.Contains(call, "/deployment-branch-policies") {
		return []byte(`{"total_count":1}`), nil
	}
	if strings.Contains(call, "/environments/") {
		if !f.env {
			return nil, fmt.Errorf("HTTP 404")
		}
		return []byte(`{"protection_rules":[{"type":"required_reviewers","reviewers":[{"id":1}]}],"deployment_branch_policy":{"custom_branch_policies":true,"protected_branches":false}}`), nil
	}
	return nil, fmt.Errorf("unexpected command %s", call)
}
func githubFixture(t *testing.T) (githubOptions, *fakeGitHub) {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0700)
	os.WriteFile(filepath.Join(root, ".github", "workflows", "ocr-bench-deploy.yml"), []byte("workflow"), 0600)
	policy := filepath.Join(root, "policy.json")
	os.WriteFile(policy, []byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::state/prefix/*"]}]}`), 0600)
	t.Setenv("PULUMI_CONFIG_PASSPHRASE", "test-only-secret")
	return githubOptions{target: fixture(t, "ocr").spec, Settings: Settings{1, "dev", "sso", "us-east-2", "123456789012", "s3://state/prefix"}, Repository: "lambdawalker/go.appexfission.aws.card-capture", Environment: "bench-dev", Project: "test", Private: root, Root: root, Policy: policy}, &fakeGitHub{variables: map[string]string{"UNRELATED": "keep"}}
}
func TestGitHubCreateRerunDriftRotation(t *testing.T) {
	o, f := githubFixture(t)
	auth := func(string) error { return nil }
	if e := reconcileGitHub(o, f.run, auth); e != nil {
		t.Fatal(e)
	}
	writes := f.writes
	if e := reconcileGitHub(o, f.run, func(string) error { t.Fatal("unchanged setup asked for approval"); return nil }); e != nil {
		t.Fatal(e)
	}
	if f.writes != writes || f.deliveries != 1 || f.variables["UNRELATED"] != "keep" {
		t.Fatal(f)
	}
	delete(f.variables, "BENCH_REGION")
	if e := reconcileGitHub(o, f.run, auth); e != nil {
		t.Fatal(e)
	}
	if f.variables["BENCH_REGION"] != "us-east-2" {
		t.Fatal("marker hid drift")
	}
	o.SecretVersion = "2"
	if e := reconcileGitHub(o, f.run, auth); e != nil {
		t.Fatal(e)
	}
	if f.deliveries != 2 {
		t.Fatal("rotation not delivered")
	}
}
func TestGitHubPartialSetup(t *testing.T) {
	o, f := githubFixture(t)
	f.fail = "secret set"
	if e := reconcileGitHub(o, f.run, func(string) error { return nil }); e == nil {
		t.Fatal("failure hidden")
	}
	if f.variables["DEPLOY_CONFIG_SHA256"] != "" {
		t.Fatal("partial setup marked complete")
	}
	f.fail = ""
	if e := reconcileGitHub(o, f.run, func(string) error { return nil }); e != nil {
		t.Fatal(e)
	}
}
func TestGitHubUnknownPermissionsNeverCreate(t *testing.T) {
	o, f := githubFixture(t)
	f.fail = "/environments/"
	if e := reconcileGitHub(o, f.run, func(string) error { return nil }); e == nil || f.writes != 0 {
		t.Fatal(e, f.writes)
	}
}
func TestGitHubDeclineNoWrites(t *testing.T) {
	o, f := githubFixture(t)
	if e := reconcileGitHub(o, f.run, func(string) error { return fmt.Errorf("cancelled") }); e == nil || f.writes != 0 {
		t.Fatal(e, f.writes)
	}
}
