package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type featureInput struct {
	spec  inputSpec
	value *string
}

type featureInputs []featureInput

func (m manifest) inputFlags(fs *flag.FlagSet) featureInputs {
	var fields featureInputs
	for _, f := range m.Inputs {
		fields = append(fields, featureInput{f, fs.String(strings.ToLower(f.Name), "", f.Label)})
	}
	return fields
}

func (inputs featureInputs) load(file, dir string, ci, reconfigure bool) error {
	if len(inputs) == 0 {
		return nil
	}
	path := filepath.Join(dir, file)
	var saved map[string]json.RawMessage
	if b, err := os.ReadFile(path); err == nil {
		var version int
		if json.Unmarshal(b, &saved) != nil || json.Unmarshal(saved["Version"], &version) != nil || version != 1 {
			return fmt.Errorf("invalid saved feature settings")
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	out := map[string]any{"Version": 1}
	for _, f := range inputs {
		var prior string
		if v, ok := saved[f.spec.Name]; ok && json.Unmarshal(v, &prior) != nil {
			return fmt.Errorf("invalid saved %s", f.spec.Name)
		}
		if *f.value == "" {
			switch {
			case ci:
				return fmt.Errorf("--%s required in CI", strings.ToLower(f.spec.Name))
			case prior != "" && !reconfigure:
				*f.value = prior
			default:
				*f.value = prompt(f.spec.Label, prior)
			}
		}
		if !f.spec.valid(*f.value) {
			return fmt.Errorf("invalid %s", f.spec.Label)
		}
		out[f.spec.Name] = *f.value
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	return atomicWrite(path, raw)
}

func (s inputSpec) valid(value string) bool {
	switch s.Kind {
	case "email":
		address, err := mail.ParseAddress(value)
		return err == nil && address.Address == value
	case "domain":
		return pattern(`^[a-z0-9][a-z0-9.-]*[a-z0-9]$`, value)
	default:
		return false
	}
}

func (inputs featureInputs) githubVariables() map[string]string {
	result := make(map[string]string, len(inputs))
	for _, f := range inputs {
		result[f.spec.GitHubVariable] = *f.value
	}
	return result
}

// Scope defaults to missing keys only. A read or decryption failure must not
// overwrite an existing value, especially when restoring a legacy stack.
func (m manifest) setMissingConfig(ctx context.Context, dir, stack string) error {
	if len(m.ConfigDefaults) == 0 {
		return nil
	}
	raw, err := outputIn(ctx, dir, "pulumi", "config", "--json", "--stack", stack)
	if err != nil {
		return err
	}
	var current map[string]json.RawMessage
	if json.Unmarshal(raw, &current) != nil || current == nil {
		return fmt.Errorf("invalid configuration inventory")
	}
	keys := make([]string, 0, len(m.ConfigDefaults))
	for key := range m.ConfigDefaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, exists := current[m.PulumiProject+":"+key]; exists {
			continue
		}
		if err = command(ctx, dir, "pulumi", "config", "set", key, m.ConfigDefaults[key], "--stack", stack, "--non-interactive"); err != nil {
			return err
		}
	}
	return nil
}
