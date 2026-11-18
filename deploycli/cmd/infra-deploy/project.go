package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type projectRecord struct {
	Version    int
	Name, Root string
}

// projectPaths persists repository identity separately from the central nonsecret registry.
func (cli deployCLI) projectPaths(root, name string, ci bool) (string, string, string, error) {
	local := filepath.Join(root, ".deploy")
	p := filepath.Join(local, "project.json")
	var r projectRecord
	if b, e := os.ReadFile(p); e == nil {
		if json.Unmarshal(b, &r) != nil || r.Version != 1 {
			return "", "", "", errors.New("invalid .deploy/project.json")
		}
		if name != "" && name != r.Name {
			return "", "", "", errors.New("--project differs from saved repository project")
		}
		if r.Root != root {
			return "", "", "", errors.New("checkout moved; follow explicit project/vault migration before continuing")
		}
		name = r.Name
	} else if !os.IsNotExist(e) {
		return "", "", "", e
	}
	if name == "" {
		if ci {
			return "", "", "", errors.New("--project required on first noninteractive run")
		}
		name = prompt("Project name", cli.spec.PulumiProject)
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`).MatchString(name) {
		return "", "", "", errors.New("project name must be lowercase letters/digits/hyphens, starting with a letter")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return "", "", "", e
	}
	central := filepath.Join(home, ".infra-composer", name)
	for _, dir := range []string{local, central} {
		if e = privateDirectory(dir); e != nil {
			return "", "", "", e
		}
	}
	unlock, e := operationLock(filepath.Join(central, "project.lock"))
	if e != nil {
		return "", "", "", e
	}
	defer unlock()
	registry := filepath.Join(central, "project.json")
	if b, e := os.ReadFile(registry); e == nil {
		var old projectRecord
		if json.Unmarshal(b, &old) != nil || old.Version != 1 || old.Name != name {
			return "", "", "", errors.New("invalid central project record")
		}
		if old.Root != root {
			return "", "", "", fmt.Errorf("project %s already belongs to %s; select a different project name", name, old.Root)
		}
	} else if !os.IsNotExist(e) {
		return "", "", "", e
	}
	r = projectRecord{1, name, root}
	b, _ := json.MarshalIndent(r, "", "  ")
	if e = atomicWrite(registry, b); e != nil {
		return "", "", "", e
	}
	if e = atomicWrite(p, b); e != nil {
		return "", "", "", e
	}
	return name, local, central, nil
}

type stackRecord struct {
	Version                         int
	Account, Region, Backend, Stack string
	Initialized                     bool
}

// ensureStack trusts a successful remote listing, never a failed select or local flag alone.
func (cli deployCLI) ensureStack(ctx context.Context, dir, stack, receipt string, target stackRecord, allowCreate bool, run func(...string) error, list func() ([]byte, error)) error {
	if b, e := os.ReadFile(receipt); e == nil {
		var prior stackRecord
		if json.Unmarshal(b, &prior) != nil || prior.Version != 1 || prior.Stack != stack {
			return errors.New("invalid stack receipt")
		}
		if prior.Account != target.Account || prior.Region != target.Region || prior.Backend != target.Backend {
			return errors.New("saved stack target differs; choose a separate project/environment instead of silently adopting resources")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	b, e := list()
	if e != nil {
		return fmt.Errorf("cannot inspect Pulumi stacks; refusing to initialize: %w", e)
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(b, &rows) != nil {
		return errors.New("invalid Pulumi stack listing")
	}
	found := false
	for _, row := range rows {
		if row.Name == stack || strings.HasSuffix(row.Name, "/"+cli.spec.PulumiProject+"/"+stack) {
			found = true
		}
	}
	if found {
		if e = run("stack", "select", stack, "--non-interactive"); e != nil {
			return e
		}
	} else {
		if b, e := os.ReadFile(receipt); e == nil {
			var prior stackRecord
			if json.Unmarshal(b, &prior) != nil || prior.Version != 1 || prior.Stack != stack {
				return errors.New("invalid stack receipt")
			}
			if prior.Initialized {
				return errors.New("previously initialized stack is missing; recover state instead of recreating")
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if !allowCreate {
			return errors.New("stack does not exist; run setup/preview/deploy to initialize it")
		}
		note("First deployment: creating Pulumi stack " + stack)
		if e = run("stack", "init", stack, "--secrets-provider", "passphrase", "--non-interactive"); e != nil {
			return e
		}
	}
	target.Version = 1
	target.Stack = stack
	target.Initialized = true
	b, e = json.MarshalIndent(target, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(receipt, b)
}
