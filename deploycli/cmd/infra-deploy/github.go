package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lambdawalker/go.infra.composer/awsnames"
)

type setupRunner func(stdin string, args ...string) ([]byte, error)
type githubOptions struct {
	target                                                                       manifest
	variables                                                                    map[string]string
	Settings                                                                     Settings
	Role, Repository, Environment, Project, Policy, SecretVersion, Private, Root string
	Force                                                                        bool
}
type githubReceipt struct {
	Version                                                   int
	Repository, Environment, Role, SecretVersion, Fingerprint string
	DeliveredAt, CompletedAt                                  time.Time
}

func setupGitHub(ctx context.Context, o githubOptions) error {
	run := func(stdin string, args ...string) ([]byte, error) {
		c := toolCommand(ctx, args...)
		if stdin != "" {
			c.Stdin = strings.NewReader(stdin)
		}
		b, e := c.CombinedOutput()
		if e != nil {
			return nil, fmt.Errorf("%s failed: %s", args[0], strings.TrimSpace(string(b)))
		}
		return b, nil
	}
	ui := wizard(ctx)
	ui.phase("prepare", "Review GitHub deployment setup")
	return reconcileGitHub(o, run, func(plan string) error {
		if ui.browser != nil {
			ui.note(plan)
		} else {
			fmt.Println(plan)
		}
		return wizard(ctx).deliberate("Type the GitHub environment to authorize these setup changes", o.Environment)
	})
}
func githubTrust(account, repository, environment string) map[string]any {
	provider := "arn:aws:iam::" + account + ":oidc-provider/token.actions.githubusercontent.com"
	return map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Sid": "InfraDeployGitHub", "Effect": "Allow", "Principal": map[string]string{"Federated": provider}, "Action": "sts:AssumeRoleWithWebIdentity", "Condition": map[string]any{"StringEquals": map[string]string{"token.actions.githubusercontent.com:aud": "sts.amazonaws.com", "token.actions.githubusercontent.com:sub": "repo:" + repository + ":environment:" + environment}}}}}
}

func canonicalHash(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func decodeObject(b []byte) (map[string]any, error) {
	var v map[string]any
	e := json.Unmarshal(b, &v)
	if e == nil && v == nil {
		e = fmt.Errorf("empty response")
	}
	return v, e
}
func reconcileGitHub(o githubOptions, run setupRunner, authorize func(string) error) (err error) {
	if o.Repository != o.target.Repository || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`).MatchString(o.Environment) {
		return fmt.Errorf("configured repository and safe GitHub environment required")
	}
	if o.SecretVersion == "" {
		o.SecretVersion = "1"
	}
	if os.Getenv("PULUMI_CONFIG_PASSPHRASE") == "" {
		return fmt.Errorf("unlock Pulumi passphrase before setup")
	}
	if o.Role == "" && o.target.GitHub.ManagedPolicy == "" {
		return fmt.Errorf("--github-role must name an existing scoped OIDC deployment role")
	}
	if o.Role == "" {
		o.Role = "arn:aws:iam::" + o.Settings.Account + ":role/" + o.Project + "-" + o.Settings.Environment + "-deploy"
	}
	prefix := "arn:aws:iam::" + o.Settings.Account + ":role/"
	name, ok := strings.CutPrefix(o.Role, prefix)
	if !ok {
		return fmt.Errorf("role must be in the selected account with a simple role name")
	}
	if err := awsnames.Validate(awsnames.IAMRole, name); err != nil {
		return err
	}
	if policy := o.target.GitHub.ManagedPolicy; policy != "" {
		if err := awsnames.Validate(awsnames.IAMInlinePolicy, policy); err != nil {
			return err
		}
	}
	base := "repos/" + o.Repository + "/environments/" + url.PathEscape(o.Environment)
	receiptPath := filepath.Join(o.Private, "github-"+o.Environment+".json")
	defer func() {
		if err != nil {
			fmt.Printf("GitHub setup INCOMPLETE. Existing settings and partial resources retained. Rerun the same command.\nManual recovery: https://github.com/%s/settings/environments\nEnvironment: %s; role: %s\nSet required reviewers and deployment branch restrictions. Set BENCH_PROJECT=%s BENCH_ACCOUNT=%s BENCH_REGION=%s BENCH_BACKEND=%s BENCH_ENVIRONMENT=%s BENCH_ROLE_ARN=%s.\nDeliver the existing passphrase with: gh secret set PULUMI_CONFIG_PASSPHRASE --repo %s --env %s (interactive hidden entry; never paste it into command arguments).\nReview the setup plan at %s; do not mark completion until every check succeeds.\n", o.Repository, o.Environment, o.Role, o.Project, o.Settings.Account, o.Settings.Region, o.Settings.Backend, o.Settings.Environment, o.Role, o.Repository, o.Environment, filepath.Join(o.Private, "github-setup-plan.json"))
		}
	}()
	b, e := run("", "gh", "api", "user")
	if e != nil {
		return fmt.Errorf("GitHub identity lookup: %w", e)
	}
	var user struct {
		Login string
		ID    int
	}
	if json.Unmarshal(b, &user) != nil || user.ID == 0 {
		return fmt.Errorf("invalid GitHub identity")
	}
	b, e = run("", "gh", "repo", "view", o.Repository, "--json", "nameWithOwner,defaultBranchRef")
	if e != nil {
		return e
	}
	var repo struct {
		NameWithOwner    string
		DefaultBranchRef struct{ Name string }
	}
	if json.Unmarshal(b, &repo) != nil || repo.NameWithOwner != o.Repository || repo.DefaultBranchRef.Name == "" {
		return fmt.Errorf("repository identity mismatch")
	}
	envBytes, e := run("", "gh", "api", base)
	fresh := e != nil && strings.Contains(e.Error(), "HTTP 404")
	if e != nil && !fresh {
		return fmt.Errorf("environment lookup unknown (administration permission required): %w", e)
	}
	if fresh && o.target.GitHub.ManagedPolicy == "" {
		return fmt.Errorf("create a protected GitHub environment before setup")
	}
	var environment map[string]any
	if !fresh {
		environment, e = decodeObject(envBytes)
		if e != nil {
			return e
		}
		if !protectedEnvironment(environment) {
			return fmt.Errorf("existing environment requires reviewers and branch restrictions; configure them in Settings > Environments without removing existing protections")
		}
	}
	provider := "arn:aws:iam::" + o.Settings.Account + ":oidc-provider/token.actions.githubusercontent.com"
	trust := githubTrust(o.Settings.Account, o.Repository, o.Environment)
	providerBytes, e := run("", "aws", "iam", "get-open-id-connect-provider", "--open-id-connect-provider-arn", provider, "--output", "json")
	createProvider := e != nil && strings.Contains(e.Error(), "NoSuchEntity")
	if e != nil && !createProvider {
		return e
	}
	if !createProvider {
		var p struct {
			Url          string
			ClientIDList []string
		}
		if json.Unmarshal(providerBytes, &p) != nil || strings.TrimPrefix(p.Url, "https://") != "token.actions.githubusercontent.com" {
			return fmt.Errorf("OIDC provider URL mismatch")
		}
		found := false
		for _, a := range p.ClientIDList {
			found = found || a == "sts.amazonaws.com"
		}
		if !found {
			return fmt.Errorf("existing OIDC provider lacks sts.amazonaws.com audience; add this audience explicitly without removing other clients")
		}
	}
	roleBytes, e := run("", "aws", "iam", "get-role", "--role-name", name, "--output", "json")
	createRole := e != nil && strings.Contains(e.Error(), "NoSuchEntity")
	if e != nil && !createRole {
		return e
	}
	if !createRole {
		var r struct {
			Role struct {
				Arn                      string
				AssumeRolePolicyDocument any
			}
		}
		if json.Unmarshal(roleBytes, &r) != nil || r.Role.Arn != o.Role {
			return fmt.Errorf("role identity mismatch")
		}
		if !compatibleTrust(r.Role.AssumeRolePolicyDocument, trust) {
			return fmt.Errorf("existing role trust differs; refusing to replace independently managed trust; reconcile exact audience and repository/environment subject manually")
		}
	}
	if o.target.GitHub.ManagedPolicy == "" && (createProvider || createRole || o.Policy != "") {
		return fmt.Errorf("this target requires an existing OIDC provider and role; manage IAM outside this setup")
	}
	var policy map[string]any
	if o.Policy != "" {
		raw, e := os.ReadFile(o.Policy)
		if e != nil {
			return e
		}
		policy, e = decodeObject(raw)
		if e != nil {
			return e
		}
		if e = validateDeploymentPolicy(policy); e != nil {
			return e
		}
	}
	if !createRole && policy == nil && o.target.GitHub.ManagedPolicy != "" {
		raw, e := run("", "aws", "iam", "get-role-policy", "--role-name", name, "--policy-name", o.target.GitHub.ManagedPolicy, "--output", "json")
		if e != nil {
			return fmt.Errorf("provide --github-policy to reconcile a reviewed managed deployment policy: %w", e)
		}
		var saved struct{ PolicyDocument map[string]any }
		if json.Unmarshal(raw, &saved) != nil {
			return fmt.Errorf("invalid existing deployment policy")
		}
		policy = saved.PolicyDocument
		if e = validateDeploymentPolicy(policy); e != nil {
			return e
		}
	}
	if createRole && policy == nil {
		return fmt.Errorf("new role requires --github-policy with a reviewed least-privilege JSON policy for the existing resource identities and state prefix")
	}
	policyChanged := false
	if policy != nil && !createRole {
		raw, e := run("", "aws", "iam", "get-role-policy", "--role-name", name, "--policy-name", o.target.GitHub.ManagedPolicy, "--output", "json")
		if e != nil && !strings.Contains(e.Error(), "NoSuchEntity") {
			return e
		}
		var p struct{ PolicyDocument any }
		if e == nil && json.Unmarshal(raw, &p) != nil {
			return fmt.Errorf("invalid IAM policy response")
		}
		policyChanged = canonicalHash(p.PolicyDocument) != canonicalHash(policy)
	} else {
		policyChanged = policy != nil
	}
	desired := map[string]string{"BENCH_PROJECT": o.Project, "BENCH_ACCOUNT": o.Settings.Account, "BENCH_REGION": o.Settings.Region, "BENCH_BACKEND": o.Settings.Backend, "BENCH_ENVIRONMENT": o.Settings.Environment, "BENCH_ROLE_ARN": o.Role}
	for key, value := range o.variables {
		desired[key] = value
	}
	plan := map[string]any{"schema": 1, "repository": o.Repository, "environment": o.Environment, "githubIdentity": user.Login, "createEnvironment": fresh, "reviewer": user.Login, "branch": repo.DefaultBranchRef.Name, "createOIDCProvider": createProvider, "createRole": createRole, "role": o.Role, "trust": trust, "managedPolicy": policy, "updateManagedPolicy": policyChanged, "variables": desired, "secretNames": []string{"PULUMI_CONFIG_PASSPHRASE"}, "secretVersion": o.SecretVersion}
	encoded, _ := json.MarshalIndent(plan, "", "  ")
	if e = atomicWrite(filepath.Join(o.Private, "github-setup-plan.json"), encoded); e != nil {
		return e
	}
	needsWrite := fresh || createProvider || createRole || policyChanged || o.Force
	if !fresh {
		values, err := githubVariables(base, run)
		if err != nil {
			return err
		}
		for k, v := range desired {
			if values[k] != v {
				needsWrite = true
			}
		}
		inventory, err := githubSecrets(base, run)
		if err != nil {
			return err
		}
		if !inventory["PULUMI_CONFIG_PASSPHRASE"] {
			needsWrite = true
		}
		var prior githubReceipt
		if raw, err := os.ReadFile(receiptPath); err == nil {
			if json.Unmarshal(raw, &prior) != nil {
				return fmt.Errorf("invalid GitHub receipt")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if prior.Repository != o.Repository || prior.Environment != o.Environment || prior.Role != o.Role || prior.SecretVersion != o.SecretVersion || prior.DeliveredAt.IsZero() {
			needsWrite = true
		}
		if err = verifyBranches(base, environment, run); err != nil {
			needsWrite = true
		}
	}
	if needsWrite {
		if e = authorize(string(encoded)); e != nil {
			return e
		}
	}

	if fresh {
		body, _ := json.Marshal(map[string]any{"reviewers": []any{map[string]any{"type": "User", "id": user.ID}}, "deployment_branch_policy": map[string]bool{"protected_branches": false, "custom_branch_policies": true}})
		if _, e = run(string(body), "gh", "api", "--method", "PUT", base, "--input", "-"); e != nil {
			return e
		}
		branch, _ := json.Marshal(map[string]string{"name": repo.DefaultBranchRef.Name, "type": "branch"})
		if _, e = run(string(branch), "gh", "api", "--method", "POST", base+"/deployment-branch-policies", "--input", "-"); e != nil {
			return e
		}
	}
	// No existing environment PUT: protections and unrelated settings are preserved.
	if createProvider {
		if _, e = run("", "aws", "iam", "create-open-id-connect-provider", "--url", "https://token.actions.githubusercontent.com", "--client-id-list", "sts.amazonaws.com"); e != nil {
			return e
		}
	}
	if createRole {
		doc, _ := json.Marshal(trust)
		if _, e = run("", "aws", "iam", "create-role", "--role-name", name, "--assume-role-policy-document", string(doc), "--description", "Infra Composer feature deployment", "--tags", "Key=Project,Value="+o.Project, "Key=Environment,Value="+o.Settings.Environment, "Key=Component,Value=github-deployment", "Key=ic:owner,Value=github.com/"+o.target.Repository, "Key=ic:managed-by,Value=infra-composer"); e != nil {
			return e
		}
	}
	if policyChanged {
		doc, _ := json.Marshal(policy)
		if _, e = run("", "aws", "iam", "put-role-policy", "--role-name", name, "--policy-name", o.target.GitHub.ManagedPolicy, "--policy-document", string(doc)); e != nil {
			return e
		}
	}
	envBytes, e = run("", "gh", "api", base)
	if e != nil {
		return e
	}
	environment, e = decodeObject(envBytes)
	if e != nil || !protectedEnvironment(environment) {
		return fmt.Errorf("environment protection verification incomplete")
	}
	if branchPolicy, ok := environment["deployment_branch_policy"].(map[string]any); ok && branchPolicy["custom_branch_policies"] == true {
		raw, err := run("", "gh", "api", base+"/deployment-branch-policies")
		if err != nil {
			return err
		}
		var branches struct {
			TotalCount int `json:"total_count"`
		}
		if json.Unmarshal(raw, &branches) != nil {
			return fmt.Errorf("invalid branch policy response")
		}
		if branches.TotalCount == 0 {
			body, _ := json.Marshal(map[string]string{"name": repo.DefaultBranchRef.Name, "type": "branch"})
			if _, err = run(string(body), "gh", "api", "--method", "POST", base+"/deployment-branch-policies", "--input", "-"); err != nil {
				return err
			}
		}
	}
	if e = verifyBranches(base, environment, run); e != nil {
		return e
	}

	workflow, e := os.ReadFile(filepath.Join(o.Root, ".github", "workflows", o.target.GitHub.Workflow))
	if e != nil {
		return e
	}
	marker := canonicalHash(map[string]any{"schema": 2, "variables": desired, "repository": o.Repository, "environment": o.Environment, "secretVersion": o.SecretVersion, "secretNames": []string{"PULUMI_CONFIG_PASSPHRASE"}, "trust": trust, "policy": policy, "protections": environment["protection_rules"], "branches": environment["deployment_branch_policy"], "workflow": string(workflow)})
	current, e := githubVariables(base, run)
	if e != nil {
		return e
	}
	names := []string{}
	for k := range desired {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if current[k] != desired[k] || o.Force {
			if e = setGitHubVariable(base, k, desired[k], current, run); e != nil {
				return e
			}
		}
	}
	secrets, e := githubSecrets(base, run)
	if e != nil {
		return e
	}
	var prior githubReceipt
	if b, e := os.ReadFile(receiptPath); e == nil {
		if json.Unmarshal(b, &prior) != nil {
			return fmt.Errorf("invalid GitHub receipt")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	delivered := prior.DeliveredAt
	if !secrets["PULUMI_CONFIG_PASSPHRASE"] || prior.Repository != o.Repository || prior.Environment != o.Environment || prior.Role != o.Role || prior.SecretVersion != o.SecretVersion || delivered.IsZero() || o.Force {
		if _, e = run(os.Getenv("PULUMI_CONFIG_PASSPHRASE"), "gh", "secret", "set", "PULUMI_CONFIG_PASSPHRASE", "--repo", o.Repository, "--env", o.Environment); e != nil {
			return fmt.Errorf("secret delivery failed (value not logged)")
		}
		delivered = time.Now().UTC()
		receipt := githubReceipt{Version: 1, Repository: o.Repository, Environment: o.Environment, Role: o.Role, SecretVersion: o.SecretVersion, DeliveredAt: delivered}
		b, _ := json.Marshal(receipt)
		if e = atomicWrite(receiptPath, b); e != nil {
			return e
		}
	}
	// Verify IAM writes and environment protections before claiming completion.
	roleCheck, e := run("", "aws", "iam", "get-role", "--role-name", name, "--output", "json")
	if e != nil {
		return e
	}
	var verified struct {
		Role struct {
			Arn                      string
			AssumeRolePolicyDocument any
		}
	}
	if json.Unmarshal(roleCheck, &verified) != nil || verified.Role.Arn != o.Role || !compatibleTrust(verified.Role.AssumeRolePolicyDocument, trust) {
		return fmt.Errorf("OIDC role trust verification incomplete")
	}
	if o.target.GitHub.ManagedPolicy != "" {
		policyCheck, e := run("", "aws", "iam", "get-role-policy", "--role-name", name, "--policy-name", o.target.GitHub.ManagedPolicy, "--output", "json")
		if e != nil {
			return e
		}
		var verifiedPolicy struct{ PolicyDocument any }
		if json.Unmarshal(policyCheck, &verifiedPolicy) != nil || canonicalHash(verifiedPolicy.PolicyDocument) != canonicalHash(policy) {
			return fmt.Errorf("managed policy verification incomplete")
		}
	}
	finalEnv, e := run("", "gh", "api", base)
	if e != nil {
		return e
	}
	finalEnvironment, e := decodeObject(finalEnv)
	if e != nil || canonicalHash(finalEnvironment["protection_rules"]) != canonicalHash(environment["protection_rules"]) || canonicalHash(finalEnvironment["deployment_branch_policy"]) != canonicalHash(environment["deployment_branch_policy"]) {
		return fmt.Errorf("environment protections changed during setup; rerun for review")
	}
	// Re-read before publishing completion; marker is never a substitute for checks.
	current, e = githubVariables(base, run)
	if e != nil {
		return e
	}
	for k, v := range desired {
		if current[k] != v {
			return fmt.Errorf("variable %s drifted during reconciliation", k)
		}
	}
	secrets, e = githubSecrets(base, run)
	if e != nil {
		return e
	}
	if !secrets["PULUMI_CONFIG_PASSPHRASE"] {
		return fmt.Errorf("secret-name inventory incomplete")
	}
	if current["DEPLOY_CONFIG_SHA256"] != marker || o.Force {
		if e = setGitHubVariable(base, "DEPLOY_CONFIG_SHA256", marker, current, run); e != nil {
			return e
		}
	}
	receipt := githubReceipt{1, o.Repository, o.Environment, o.Role, o.SecretVersion, marker, delivered, time.Now().UTC()}
	b, _ = json.Marshal(receipt)
	if e = atomicWrite(receiptPath, b); e != nil {
		return e
	}
	fmt.Printf("GitHub setup completed for %s. Secret delivery receipt retained; stored values cannot be verified.\nhttps://github.com/%s/actions/workflows/%s\n", o.Environment, o.Repository, o.target.GitHub.Workflow)
	return nil
}
func protectedEnvironment(env map[string]any) bool {
	rules, _ := env["protection_rules"].([]any)
	reviewed := false
	for _, raw := range rules {
		r, _ := raw.(map[string]any)
		v, _ := r["reviewers"].([]any)
		if r["type"] == "required_reviewers" && len(v) > 0 {
			reviewed = true
		}
	}
	return reviewed && env["deployment_branch_policy"] != nil
}
func verifyBranches(base string, env map[string]any, run setupRunner) error {
	p, _ := env["deployment_branch_policy"].(map[string]any)
	if p["protected_branches"] == true {
		return nil
	}
	b, e := run("", "gh", "api", base+"/deployment-branch-policies")
	if e != nil {
		return e
	}
	var r struct {
		TotalCount int `json:"total_count"`
	}
	if json.Unmarshal(b, &r) != nil || r.TotalCount == 0 {
		return fmt.Errorf("no deployment branch restrictions; add the reviewed default branch in environment settings")
	}
	return nil
}
func compatibleTrust(actual any, desired map[string]any) bool {
	// Sid is descriptive; an existing otherwise exact trust remains compatible.
	normalize := func(v any) string {
		b, _ := json.Marshal(v)
		var obj map[string]any
		if json.Unmarshal(b, &obj) != nil {
			return "invalid"
		}
		if statements, ok := obj["Statement"].([]any); ok {
			for _, s := range statements {
				if m, ok := s.(map[string]any); ok {
					delete(m, "Sid")
				}
			}
		}
		return canonicalHash(obj)
	}
	return normalize(actual) == normalize(desired)
}
func validateDeploymentPolicy(p map[string]any) error {
	rows, ok := p["Statement"].([]any)
	if !ok || len(rows) == 0 {
		return fmt.Errorf("reviewed IAM policy requires Statement array")
	}
	for _, raw := range rows {
		r, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid policy statement")
		}
		if r["Effect"] != "Allow" {
			continue
		}
		if r["NotAction"] != nil || r["NotResource"] != nil {
			return fmt.Errorf("deployment policy must use explicit Action and Resource")
		}
		b, _ := json.Marshal(r["Action"])
		if strings.Contains(string(b), `"*"`) || strings.Contains(string(b), `:*"`) {
			return fmt.Errorf("deployment policy must enumerate actions; wildcard services/actions rejected")
		}
		resources, _ := json.Marshal(r["Resource"])
		if strings.Contains(string(resources), `"*"`) && r["Condition"] == nil {
			return fmt.Errorf("wildcard resources require reviewed conditions; split discovery/read-only permissions into a separately reviewed account policy")
		}
		if r["Resource"] == nil || r["Action"] == nil {
			return fmt.Errorf("deployment policy needs actions/resources")
		}
	}
	return nil
}
func githubVariables(base string, run setupRunner) (map[string]string, error) {
	b, e := run("", "gh", "api", base+"/variables?per_page=100", "--paginate", "--slurp")
	if e != nil {
		return nil, e
	}
	var pages []struct {
		Variables []struct{ Name, Value string }
	}
	if e = json.Unmarshal(b, &pages); e != nil {
		return nil, e
	}
	m := map[string]string{}
	for _, p := range pages {
		for _, v := range p.Variables {
			m[v.Name] = v.Value
		}
	}
	return m, nil
}
func githubSecrets(base string, run setupRunner) (map[string]bool, error) {
	b, e := run("", "gh", "api", base+"/secrets?per_page=100", "--paginate", "--slurp")
	if e != nil {
		return nil, e
	}
	var pages []struct{ Secrets []struct{ Name string } }
	if e = json.Unmarshal(b, &pages); e != nil {
		return nil, e
	}
	m := map[string]bool{}
	for _, p := range pages {
		for _, v := range p.Secrets {
			m[v.Name] = true
		}
	}
	return m, nil
}
func setGitHubVariable(base, k, v string, current map[string]string, run setupRunner) error {
	method := "POST"
	endpoint := base + "/variables"
	if _, ok := current[k]; ok {
		method = "PATCH"
		endpoint += "/" + k
	}
	body, _ := json.Marshal(map[string]string{"name": k, "value": v})
	_, e := run(string(body), "gh", "api", "--method", method, endpoint, "--input", "-")
	return e
}
