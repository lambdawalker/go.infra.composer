package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

func (o *githubOptions) collectGitHub(environment string, explicit bool) (bool, error) {
	path := filepath.Join(o.Private, "github-settings.json")
	var saved githubOptions
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &saved); err != nil {
			return false, err
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if !explicit && saved.Repository == "" {
		if _, err := os.Stat(filepath.Join(o.Private, "github-skipped")); err == nil {
			return false, nil
		}
		heading("Configure GitHub deployment support? [y/N]")
		yes, err := timedChoice(false)
		if err != nil || !yes {
			return false, err
		}
	}
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
		o.Environment = prompt("GitHub protected environment", o.target.GitHub.EnvironmentPrefix+environment)
	}
	if o.Role == "" && o.target.GitHub.ManagedPolicy == "" {
		o.Role = prompt("Existing scoped GitHub OIDC role ARN", "")
	}
	if o.target.GitHub.ManagedPolicy != "" && o.Policy == "" && saved.Repository == "" {
		o.Policy = prompt("Reviewed deployment IAM policy file (Enter to reuse an existing policy)", "")
	}
	if o.SecretVersion == "" {
		o.SecretVersion = "1"
	}
	return true, nil
}

func (o *githubOptions) reviewGitHub() []reviewField {
	return []reviewField{
		{label: "GitHub repository", value: func() string { return o.Repository }},
		textReviewField("GitHub environment", &o.Environment, "", func(s string) bool {
			return pattern(`^[a-zA-Z0-9_-]{1,80}$`, s)
		}),
		textReviewField("GitHub OIDC role", &o.Role, "", func(s string) bool {
			return s == "" || pattern(`^arn:aws:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_-]{1,64}$`, s)
		}),
		textReviewField("GitHub IAM policy file", &o.Policy, "", nil),
		textReviewField("GitHub secret version", &o.SecretVersion, "", nil),
	}
}

func (o githubOptions) applyGitHub(ctx context.Context) error {
	if os.Getenv("GH_TOKEN") == "" && os.Getenv("GITHUB_TOKEN") == "" {
		if _, err := output(ctx, "gh", "auth", "status", "--hostname", "github.com"); err != nil {
			if err = command(ctx, o.Root, "gh", "auth", "login", "--hostname", "github.com", "--web"); err != nil {
				return err
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
