// Package pulumicli implements teardown using the Pulumi and AWS executables.
// Unsupported resources deliberately fail verification rather than imply absence.
package pulumicli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lambdawalker/go.infra.composer/teardown"
)

type Project struct {
	Name, Directory, Backend, Region string
	Shared                           bool
}
type Config struct {
	Scope    teardown.Scope
	Projects []Project
	Recovery []teardown.Artifact
}
type Runner interface {
	Run(context.Context, string, []string, string, ...string) ([]byte, error)
}
type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Env = os.Environ()
	for _, v := range env {
		key := strings.SplitN(v, "=", 2)[0] + "="
		filtered := c.Env[:0]
		for _, old := range c.Env {
			if !strings.HasPrefix(old, key) {
				filtered = append(filtered, old)
			}
		}
		c.Env = append(filtered, v)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	e := c.Run()
	if e != nil {
		return append(stdout.Bytes(), stderr.Bytes()...), e
	}
	return stdout.Bytes(), nil
}

type Driver struct {
	Config           Config
	Runner           Runner
	recoveryPrepared bool
}

func New(c Config, r Runner) (*Driver, error) {
	if c.Scope.Application == "" || !regexp.MustCompile(`^[0-9]{12}$`).MatchString(c.Scope.Account) {
		return nil, errors.New("application and twelve-digit AWS account required")
	}
	if len(c.Projects) == 0 {
		return nil, errors.New("configured projects required")
	}
	seen := map[string]bool{}
	for _, p := range c.Projects {
		if p.Name == "" || p.Directory == "" || p.Backend == "" || p.Region == "" {
			return nil, errors.New("project name, directory, backend and region required")
		}
		k := p.Backend + "\x00" + p.Name
		if seen[k] {
			return nil, errors.New("duplicate project")
		}
		seen[k] = true
	}
	if r == nil {
		r = OSRunner{}
	}
	return &Driver{Config: c, Runner: r}, nil
}

var codeRE = regexp.MustCompile(`\(([A-Za-z0-9_.]+)\) when calling`)

func cliError(op string, b []byte) error {
	code := codeRE.FindSubmatch(b)
	if len(code) > 1 {
		return fmt.Errorf("%s: %s", op, code[1])
	}
	return fmt.Errorf("%s failed", op)
}
func (d *Driver) aws(ctx context.Context, region string, args ...string) ([]byte, error) {
	a := append([]string{}, args...)
	a = append(a, "--output", "json", "--no-cli-pager")
	b, e := d.Runner.Run(ctx, "", []string{"AWS_REGION=" + region, "AWS_DEFAULT_REGION=" + region, "AWS_PAGER="}, "aws", a...)
	if e != nil {
		return nil, cliError("aws "+strings.Join(args[:min(2, len(args))], " "), b)
	}
	return b, nil
}
func (d *Driver) identity(ctx context.Context) error {
	b, e := d.aws(ctx, d.Config.Projects[0].Region, "sts", "get-caller-identity")
	if e != nil {
		return e
	}
	var v struct{ Account string }
	if json.Unmarshal(b, &v) != nil || v.Account != d.Config.Scope.Account {
		return errors.New("AWS account does not match scope")
	}
	return nil
}
func (d *Driver) pulumi(ctx context.Context, t teardown.Target, args ...string) ([]byte, error) {
	b, e := d.Runner.Run(ctx, t.Directory, []string{"PULUMI_BACKEND_URL=" + t.Backend, "AWS_REGION=" + t.Region, "AWS_DEFAULT_REGION=" + t.Region}, "pulumi", args...)
	if e != nil {
		return nil, cliError("pulumi "+strings.Join(args[:min(2, len(args))], " "), b)
	}
	return b, nil
}
func target(p Project, s string) teardown.Target {
	return teardown.Target{ID: p.Backend + "|" + p.Name + "|" + s, Environment: s, Region: p.Region, Backend: p.Backend, Project: p.Name, Stack: s, Directory: p.Directory, Shared: p.Shared}
}
func (d *Driver) configured(t teardown.Target) bool {
	for _, p := range d.Config.Projects {
		if p.Name == t.Project && p.Backend == t.Backend && p.Directory == t.Directory && p.Region == t.Region && t.Stack != "" {
			return true
		}
	}
	return false
}
func (d *Driver) list(ctx context.Context, t teardown.Target, all bool) ([]string, error) {
	args := []string{"stack", "ls", "--json", "--non-interactive"}
	if all {
		args = append(args, "--all")
	} else {
		args = append(args, "--project", t.Project)
	}
	b, e := d.pulumi(ctx, t, args...)
	if e != nil {
		if u, parseErr := url.Parse(t.Backend); parseErr == nil && u.Scheme == "s3" && u.Host != "" {
			_, headErr := d.aws(ctx, t.Region, "s3api", "head-bucket", "--bucket", u.Host, "--expected-bucket-owner", d.Config.Scope.Account)
			if missing(headErr, "NoSuchBucket", "404", "NotFound") {
				return []string{}, nil
			}
			if headErr != nil {
				return nil, headErr
			}
		}
		return nil, e
	}
	var rows []struct{ Name string }
	if json.Unmarshal(b, &rows) != nil || rows == nil {
		return nil, errors.New("invalid stack listing")
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Name == "" {
			return nil, errors.New("invalid stack name")
		}
		out = append(out, r.Name)
	}
	return out, nil
}
func (d *Driver) Discover(ctx context.Context, s teardown.Scope) ([]teardown.Target, error) {
	if s != d.Config.Scope {
		return nil, errors.New("scope mismatch")
	}
	if e := d.identity(ctx); e != nil {
		return nil, e
	}
	var out []teardown.Target
	var issues []teardown.Artifact
	for _, p := range d.Config.Projects {
		t := target(p, "")
		stacks, e := d.list(ctx, t, false)
		if e != nil {
			issues = append(issues, teardown.Artifact{ID: p.Backend + "|" + p.Name, Kind: "inventory", Region: p.Region, Data: map[string]string{"discovery": "true", "backend": p.Backend, "project": p.Name}})
			continue
		}
		for _, s := range stacks {
			out = append(out, target(p, s))
		}
	}
	if len(issues) > 0 {
		return out, &teardown.DiscoveryError{Artifacts: issues}
	}
	return out, nil
}

type resource struct {
	Type, URN, ID, Provider string
	External                bool
	Custom                  *bool
	Outputs, Inputs         map[string]json.RawMessage
}
type export struct {
	Deployment *struct {
		Resources         []resource
		PendingOperations json.RawMessage       `json:"pending_operations"`
		Secrets           struct{ Type string } `json:"secrets_providers"`
	}
}

func (d *Driver) state(ctx context.Context, t teardown.Target) ([]byte, []resource, error) {
	if !d.configured(t) {
		return nil, nil, errors.New("unconfigured target")
	}
	names, e := d.list(ctx, t, false)
	if e != nil {
		return nil, nil, e
	}
	found := false
	for _, n := range names {
		if n == t.Stack {
			found = true
		}
	}
	if !found {
		return nil, nil, teardown.ErrAbsent
	}
	b, e := d.pulumi(ctx, t, "stack", "export", "--stack", t.Stack, "--non-interactive")
	if e != nil {
		return nil, nil, e
	}
	var x export
	if json.Unmarshal(b, &x) != nil || x.Deployment == nil || x.Deployment.Resources == nil {
		return nil, nil, errors.New("invalid Pulumi export")
	}
	if raw := x.Deployment.PendingOperations; len(raw) > 0 {
		var operations []json.RawMessage
		if json.Unmarshal(raw, &operations) != nil || operations == nil {
			return nil, nil, errors.New("invalid pending operations in export")
		}
		if len(operations) > 0 {
			return nil, nil, errors.New("pending Pulumi operations require reconciliation before teardown")
		}
	}
	for _, r := range x.Deployment.Resources {
		if r.Custom == nil {
			return nil, nil, errors.New("exported resource custom flag missing")
		}
	}
	if x.Deployment.Secrets.Type != "" && x.Deployment.Secrets.Type != "passphrase" {
		for _, r := range x.Deployment.Resources {
			if r.Type == "aws:kms/key:Key" {
				return nil, nil, errors.New("cloud secrets provider and managed KMS resources require separate recovery ownership")
			}
		}
	}
	if e = d.ownership(t, x.Deployment.Resources); e != nil {
		return nil, nil, e
	}
	regions, e := providerRegions(t, x.Deployment.Resources)
	if e != nil {
		return nil, nil, e
	}
	for _, r := range x.Deployment.Resources {
		if _, e = resourceTarget(t, r, regions); e != nil {
			return nil, nil, e
		}
	}
	return b, x.Deployment.Resources, nil
}
func (d *Driver) ownership(t teardown.Target, rs []resource) error {
	for _, r := range rs {
		// A backend bucket cannot be removed through Pulumi while it still stores
		// the transaction state. It must be outside these target stacks and removed
		// only by the final recovery phase.
		if r.Type == "aws:s3/bucket:Bucket" || r.Type == "aws:s3/bucketV2:BucketV2" {
			for _, p := range d.Config.Projects {
				u, e := url.Parse(p.Backend)
				if e == nil && u.Scheme == "s3" && r.ID != "" && u.Host == r.ID {
					return errors.New("backend bucket is managed by a target stack; separate recovery ownership required")
				}
			}
		}
		for _, a := range d.Config.Recovery {
			if a.Kind == "recovery_key" && r.Type == "aws:kms/key:Key" && (kmsID(r.ID) == kmsID(a.ID) || strings.Contains(a.ID, "alias/")) {
				return errors.New("recovery key is managed by a target stack; separate recovery ownership required")
			}
		}
		parts := strings.Split(r.URN, "::")
		stack := t.Stack
		if i := strings.LastIndex(stack, "/"); i >= 0 {
			stack = stack[i+1:]
		}
		if len(parts) < 4 || parts[0] != "urn:pulumi:"+stack || parts[1] != t.Project {
			return errors.New("foreign resource URN")
		}
		for _, m := range []map[string]json.RawMessage{r.Inputs, r.Outputs} {
			for k, raw := range m {
				var s string
				_ = json.Unmarshal(raw, &s)
				if strings.HasPrefix(r.Type, "pulumi:providers:aws") {
					switch k {
					case "profile", "accessKey", "secretKey", "token", "assumeRole", "assumeRoles", "assumeRoleWithWebIdentity", "sharedCredentialsFile", "sharedCredentialsFiles", "sharedConfigFiles", "endpoints":
						if configuredValue(raw) {
							return errors.New("provider credential or endpoint override prohibited")
						}
					case "skipCredentialsValidation", "skipRequestingAccountId":
						if configuredValue(raw) {
							return errors.New("unsafe provider validation override")
						}
					}
				}
				if strings.HasPrefix(r.Type, "pulumi:providers:aws") && k == "allowedAccountIds" {
					var ids []string
					if json.Unmarshal(raw, &ids) != nil {
						return errors.New("invalid allowed account identifiers")
					}
					for _, id := range ids {
						if id != d.Config.Scope.Account {
							return errors.New("foreign allowed account identifier")
						}
					}
				}
				if strings.HasPrefix(r.Type, "pulumi:providers:aws") && k == "region" && s != "" && !regionPattern.MatchString(s) {
					return errors.New("invalid AWS provider region")
				}
				if k == "tags" {
					var tags map[string]string
					_ = json.Unmarshal(raw, &tags)
					if app, ok := tags["ic:application"]; ok && app != d.Config.Scope.Application {
						return errors.New("foreign application tag")
					}
				}
				var v any
				if json.Unmarshal(raw, &v) == nil {
					if e := d.checkARN(v); e != nil {
						return e
					}
				}
			}
		}
	}
	return nil
}
func (d *Driver) checkARN(v any) error {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "arn:") {
			p := strings.SplitN(x, ":", 6)
			if len(p) == 6 && p[4] != "" && p[4] != d.Config.Scope.Account && !(p[2] == "iam" && p[4] == "aws" && strings.HasPrefix(p[5], "policy/")) {
				return errors.New("foreign AWS account ARN")
			}
		}
	case []any:
		for _, y := range x {
			if e := d.checkARN(y); e != nil {
				return e
			}
		}
	case map[string]any:
		for _, y := range x {
			if e := d.checkARN(y); e != nil {
				return e
			}
		}
	}
	return nil
}
func routing(t teardown.Target) map[string]string {
	return map[string]string{"backend": t.Backend, "project": t.Project, "stack": t.Stack, "directory": t.Directory}
}
func (d *Driver) Inventory(ctx context.Context, t teardown.Target) ([]teardown.Artifact, error) {
	if e := d.identity(ctx); e != nil {
		return nil, e
	}
	_, rs, e := d.state(ctx, t)
	if e != nil {
		return nil, e
	}
	regions, e := providerRegions(t, rs)
	if e != nil {
		return nil, e
	}
	var out []teardown.Artifact
	for _, r := range rs {
		if r.Custom != nil && !*r.Custom {
			continue
		}
		if strings.HasPrefix(r.Type, "pulumi:") {
			continue
		}
		rt, err := resourceTarget(t, r, regions)
		if err != nil {
			return nil, err
		}
		a := probe(r, rt)
		out = append(out, a)
		extra, err := d.auxiliary(ctx, rt, r)
		if err != nil {
			return nil, err
		}
		out = append(out, extra...)
	}
	return out, nil
}
func guards(b []byte) ([]byte, bool, error) {
	var s map[string]json.RawMessage
	if json.Unmarshal(b, &s) != nil {
		return nil, false, errors.New("invalid state")
	}
	var dep map[string]json.RawMessage
	_ = json.Unmarshal(s["deployment"], &dep)
	var rs []map[string]json.RawMessage
	if json.Unmarshal(dep["resources"], &rs) != nil {
		return nil, false, errors.New("invalid resources")
	}
	changed := false
	for _, r := range rs {
		if string(r["external"]) == "true" {
			continue
		}
		for _, k := range []string{"protect", "retainOnDelete"} {
			if string(r[k]) == "true" {
				r[k] = json.RawMessage("false")
				changed = true
			}
		}
	}
	dep["resources"], _ = json.Marshal(rs)
	s["deployment"], _ = json.Marshal(dep)
	out, e := json.Marshal(s)
	return out, changed, e
}
func (d *Driver) Destroy(ctx context.Context, t teardown.Target) error {
	if e := d.identity(ctx); e != nil {
		return e
	}
	_, _, e := d.state(ctx, t)
	if e != nil {
		return e
	}
	_, _ = d.pulumi(ctx, t, "refresh", "--stack", t.Stack, "--yes", "--skip-preview", "--non-interactive")
	b, rs, e := d.state(ctx, t)
	if e != nil {
		return e
	}
	regions, e := providerRegions(t, rs)
	if e != nil {
		return e
	}
	var prepErrors []error
	for _, r := range rs {
		if r.External || (r.Custom != nil && !*r.Custom) {
			continue
		}
		rt, err := resourceTarget(t, r, regions)
		if err != nil {
			return err
		}
		a := probe(r, rt)
		switch a.Kind {
		case "bucket":
			if r.Type != "aws:s3/bucketV2:BucketV2" && r.Type != "aws:s3/bucket:Bucket" {
				continue
			}
			if e = d.purge(ctx, a); e != nil {
				prepErrors = append(prepErrors, e)
			}
		case "table":
			if _, e = d.aws(ctx, a.Region, "dynamodb", "update-table", "--table-name", a.ID, "--no-deletion-protection-enabled"); e != nil && !missing(e, "ResourceNotFoundException") {
				prepErrors = append(prepErrors, e)
			}
		}
	}
	modified, changed, e := guards(b)
	if e != nil {
		return e
	}
	if changed {
		dir, e := os.MkdirTemp("", "composer-state-")
		if e != nil {
			return errors.New("create state staging directory failed")
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "state.json")
		if e = os.WriteFile(path, modified, 0600); e != nil {
			return errors.New("write state staging file failed")
		}
		if _, e = d.pulumi(ctx, t, "stack", "import", "--stack", t.Stack, "--file", path, "--non-interactive"); e != nil {
			return e
		}
	}
	_, destroyErr := d.pulumi(ctx, t, "destroy", "--stack", t.Stack, "--yes", "--skip-preview", "--continue-on-error", "--non-interactive")
	destroyErr = errors.Join(append(prepErrors, destroyErr)...)
	_, remaining, e := d.state(ctx, t)
	if errors.Is(e, teardown.ErrAbsent) {
		return destroyErr
	}
	if e != nil {
		return e
	}
	for _, r := range remaining {
		if r.Custom != nil && !*r.Custom {
			continue
		}
		if r.Type != "pulumi:pulumi:Stack" && !strings.HasPrefix(r.Type, "pulumi:providers:") {
			if destroyErr != nil {
				return destroyErr
			}
			return errors.New("stack resources remain")
		}
	}
	if _, e = d.pulumi(ctx, t, "stack", "rm", "--stack", t.Stack, "--yes", "--non-interactive"); e != nil {
		return e
	}
	return destroyErr
}
func missing(e error, codes ...string) bool {
	if e == nil {
		return false
	}
	for _, c := range codes {
		if strings.HasSuffix(e.Error(), ": "+c) {
			return true
		}
	}
	return false
}

// configuredValue treats Pulumi's string-encoded false and empty defaults like
// their JSON equivalents, while rejecting encrypted or otherwise opaque values.
func configuredValue(raw json.RawMessage) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return true
	}
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != "" && x != "false" && x != "[]" && x != "{}"
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func kmsID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

var regionPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`)

func providerRegions(t teardown.Target, resources []resource) (map[string]string, error) {
	result := map[string]string{}
	for _, r := range resources {
		if r.Type != "pulumi:providers:aws" {
			continue
		}
		region := ""
		for _, m := range []map[string]json.RawMessage{r.Inputs, r.Outputs} {
			raw, ok := m["region"]
			if !ok {
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return nil, errors.New("invalid AWS provider region")
			}
			if value != "" {
				if !regionPattern.MatchString(value) {
					return nil, errors.New("invalid AWS provider region")
				}
				if region != "" && region != value {
					return nil, errors.New("conflicting AWS provider regions")
				}
				region = value
			}
		}
		if region == "" {
			region = t.Region
		}
		result[r.URN+"::"+r.ID] = region
	}
	return result, nil
}
func resourceTarget(t teardown.Target, r resource, regions map[string]string) (teardown.Target, error) {
	if strings.HasPrefix(r.Type, "aws:") && r.Provider != "" {
		region, ok := regions[r.Provider]
		if !ok {
			return t, errors.New("AWS resource provider reference unresolved")
		}
		t.Region = region
	}
	return t, nil
}
