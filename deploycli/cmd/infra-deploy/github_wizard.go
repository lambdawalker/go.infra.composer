package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

func githubWizard(ctx context.Context, o githubOptions, explicit bool) error {
	path := filepath.Join(o.Private, "github-settings.json")
	var saved githubOptions
	if b, e := os.ReadFile(path); e == nil {
		if e = json.Unmarshal(b, &saved); e != nil {
			return e
		}
		if o.Repository == "" {
			o.Repository = saved.Repository
		}
		if o.Environment == "" {
			o.Environment = saved.Environment
		}
		if o.Role == "" {
			o.Role = saved.Role
		}
		if o.Policy == "" {
			o.Policy = saved.Policy
		}
		if o.SecretVersion == "" {
			o.SecretVersion = saved.SecretVersion
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if !explicit && saved.Repository == "" {
		// Remember an explicit skip separately; reconfigure/github-setup revisits it.
		if _, e := os.Stat(filepath.Join(o.Private, "github-skipped")); e == nil {
			return nil
		}
		heading("Configure GitHub deployment support? [y/N]")
		yes, e := timedChoice(false)
		if e != nil {
			return e
		}
		if !yes {
			return atomicWrite(filepath.Join(o.Private, "github-skipped"), []byte("skipped\n"))
		}
	}
	if o.Repository == "" {
		o.Repository = o.target.Repository
	}
	if o.Environment == "" {
		o.Environment = prompt("GitHub protected environment", o.target.GitHub.EnvironmentPrefix+o.Settings.Environment)
	}
	if o.target.GitHub.ManagedPolicy != "" && o.Policy == "" && saved.Repository == "" {
		o.Policy = prompt("Reviewed deployment IAM policy file (empty reuses existing managed policy)", "")
	}
	if o.SecretVersion == "" {
		o.SecretVersion = "1"
	}
	if e := setupGitHub(ctx, o); e != nil {
		return e
	}
	b, e := json.Marshal(o)
	if e != nil {
		return e
	}
	return atomicWrite(path, b)
}
