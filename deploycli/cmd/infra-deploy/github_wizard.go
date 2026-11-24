package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

func (o *githubOptions) collectGitHub(ctx context.Context, environment string, explicit bool) (bool, error) {
	saved, err := readGitHubSettings(o.Private)
	if err != nil {
		return false, err
	}
	if !explicit && saved.Repository == "" {
		if _, err := os.Stat(filepath.Join(o.Private, "github-skipped")); err == nil {
			return false, nil
		}
		yes, err := wizard(ctx).choose("Configure GitHub deployment support? [y/N]", "Configure GitHub", false)
		if err != nil || !yes {
			return false, err
		}
	}
	hadEnvironment := o.Environment != "" || saved.Environment != ""
	o.fillDefaults(saved, environment)
	if !hadEnvironment {
		o.Environment = wizard(ctx).prompt("GitHub protected environment", o.Environment)
	}
	if o.Role == "" && o.target.GitHub.ManagedPolicy == "" {
		o.Role = wizard(ctx).prompt("Existing scoped GitHub OIDC role ARN", "")
	}
	if o.target.GitHub.ManagedPolicy != "" && o.Policy == "" && saved.Repository == "" {
		o.Policy = wizard(ctx).prompt("Reviewed deployment IAM policy file (Enter to reuse an existing policy)", "")
	}
	return true, nil
}

func readGitHubSettings(private string) (githubOptions, error) {
	path := filepath.Join(private, "github-settings.json")
	var saved githubOptions
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &saved); err != nil {
			return saved, err
		}
	} else if !os.IsNotExist(err) {
		return saved, err
	}
	return saved, nil
}

func (o *githubOptions) fillDefaults(saved githubOptions, environment string) {
	for _, pair := range []struct {
		value *string
		saved string
	}{
		{&o.Repository, saved.Repository}, {&o.Environment, saved.Environment},
		{&o.Role, saved.Role}, {&o.Policy, saved.Policy}, {&o.SecretVersion, saved.SecretVersion},
	} {
		if *pair.value == "" {
			*pair.value = pair.saved
		}
	}
	if o.Repository == "" {
		o.Repository = o.target.Repository
	}
	if o.Environment == "" {
		o.Environment = o.target.GitHub.EnvironmentPrefix + environment
	}
	if o.SecretVersion == "" {
		o.SecretVersion = "1"
	}
}

func (o githubOptions) formFields(required, enabled bool) []wizardweb.Field {
	var fields []wizardweb.Field
	if !required {
		value := "no"
		if enabled {
			value = "yes"
		}
		fields = append(fields, wizardweb.Field{Name: "github.enabled", Group: "GitHub", Label: "Configure GitHub deployment", Value: value,
			Options: []wizardweb.Option{{Value: "no", Label: "Skip"}, {Value: "yes", Label: "Configure"}}})
	}
	fields = append(fields,
		wizardweb.Field{Name: "github.repository", Group: "GitHub", Label: "Repository", Value: o.Repository, ReadOnly: true},
		wizardweb.Field{Name: "github.environment", Group: "GitHub", Label: "Protected environment", Value: o.Environment},
		wizardweb.Field{Name: "github.role", Group: "GitHub", Label: "OIDC role ARN", Value: o.Role},
	)
	if o.target.GitHub.ManagedPolicy != "" || o.Policy != "" {
		fields = append(fields, wizardweb.Field{Name: "github.policy", Group: "GitHub", Label: "IAM policy file", Value: o.Policy, Help: "Leave empty to reuse the existing managed policy."})
	}
	return append(fields, wizardweb.Field{Name: "github.secretVersion", Group: "GitHub", Label: "Secret version", Value: o.SecretVersion})
}

func (o *githubOptions) readForm(values, issues map[string]string) {
	o.Environment, o.Role = values["github.environment"], values["github.role"]
	o.Policy, o.SecretVersion = values["github.policy"], values["github.secretVersion"]
	if !pattern(`^[a-zA-Z0-9_-]{1,80}$`, o.Environment) {
		issues["github.environment"] = "Use letters, digits, hyphens, or underscores."
	}
	if (o.Role != "" || o.target.GitHub.ManagedPolicy == "") && !pattern(`^arn:aws:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_-]{1,64}$`, o.Role) {
		issues["github.role"] = fmt.Sprintf("Enter an IAM role ARN for %s.", o.Repository)
	}
	if o.SecretVersion == "" {
		o.SecretVersion = "1"
	}
}

func (o *githubOptions) reviewGitHub(ctx context.Context) []reviewField {
	return []reviewField{
		{label: "GitHub repository", value: func() string { return o.Repository }},
		textReviewField(ctx, "GitHub environment", &o.Environment, "", func(s string) bool {
			return pattern(`^[a-zA-Z0-9_-]{1,80}$`, s)
		}),
		textReviewField(ctx, "GitHub OIDC role", &o.Role, "", func(s string) bool {
			return s == "" || pattern(`^arn:aws:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_-]{1,64}$`, s)
		}),
		textReviewField(ctx, "GitHub IAM policy file", &o.Policy, "", nil),
		textReviewField(ctx, "GitHub secret version", &o.SecretVersion, "", nil),
	}
}

func (o githubOptions) applyGitHub(ctx context.Context) error {
	if !credentialsFrom(ctx).configured(githubField) {
		if _, err := output(ctx, "gh", "auth", "status", "--hostname", "github.com"); err != nil {
			args := []string{"gh", "auth", "login", "--hostname", "github.com", "--web"}
			if wizard(ctx).browser != nil {
				args = append(args, "--git-protocol", "https")
			}
			if err = wizard(ctx).login(o.Root, "GitHub", args...); err != nil {
				return err
			}
			if ui := wizard(ctx); ui.browser != nil {
				ui.note("Signed in to GitHub")
			}
		}
	}
	if err := setupGitHub(ctx, o); err != nil {
		return err
	}
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(o.Private, "github-settings.json"), b)
}
