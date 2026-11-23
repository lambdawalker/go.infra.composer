package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/dnsprovider"
)

type featureInput struct {
	plannedInput
	flag  *string
	raw   string
	value string
}

type featureInputs []featureInput

func (m manifest) inputFlags(fs *flag.FlagSet) featureInputs {
	fields := make(featureInputs, len(m.inputPlan))
	for i, plan := range m.inputPlan {
		fields[i].plannedInput = plan
		if plan.acceptsInput() {
			fields[i].flag = fs.String(strings.ToLower(plan.spec.Name), "", plan.spec.Label)
		}
	}
	return fields
}

type inputPrompt func(label, fallback, display string) (string, error)

func (inputs featureInputs) collect(ctx context.Context, file, dir string, ci, reconfigure bool, ask inputPrompt) error {
	defaults, err := inputs.defaults(file, dir)
	if err != nil {
		return err
	}
	values := make(map[string]string, len(inputs))
	for i := range inputs {
		field := &inputs[i]
		var err error
		switch {
		case field.flag == nil:
			field.value, err = field.resolve("", values)
		case *field.flag != "":
			field.raw = *field.flag
			field.value, err = field.resolve(field.raw, values)
		case ci:
			return fmt.Errorf("--%s required in CI", strings.ToLower(field.spec.Name))
		default:
			prior := defaults[field.spec.Name]
			if prior != "" && !reconfigure {
				field.raw = prior
				field.value, err = field.resolve(prior, values)
				if err == nil || field.spec.Compose == nil {
					break
				}
			}
			field.raw, field.value, err = field.read(ctx, prior, values, ask)
		}
		if err != nil {
			return err
		}
		if field.spec.Compose == nil {
			field.raw = field.value
		}
		values[field.spec.Name] = field.value
	}
	return nil
}

func (inputs featureInputs) defaults(file, dir string) (map[string]string, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, file))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var saved map[string]json.RawMessage
	var version int
	if json.Unmarshal(b, &saved) != nil || json.Unmarshal(saved["Version"], &version) != nil || version != 1 {
		return nil, fmt.Errorf("invalid saved feature settings")
	}
	var savedRaw map[string]string
	if data, ok := saved["ComposeInputs"]; ok && json.Unmarshal(data, &savedRaw) != nil {
		return nil, fmt.Errorf("invalid saved composition inputs")
	}
	defaults := make(map[string]string, len(inputs))
	for _, field := range inputs {
		name := field.spec.Name
		if field.flag == nil {
			continue
		}
		if *field.flag != "" || field.spec.Compose != nil {
			defaults[name] = savedRaw[name]
			continue
		}
		var value string
		if data, ok := saved[name]; ok && json.Unmarshal(data, &value) != nil {
			return nil, fmt.Errorf("invalid saved %s", name)
		}
		defaults[name] = value
	}
	return defaults, nil
}

func (field featureInput) read(ctx context.Context, prior string, values map[string]string, ask inputPrompt) (string, string, error) {
	fallback, display := prior, prior
	if field.spec.Compose != nil {
		display, _ = field.resolve(fallback, values)
		if display == "" {
			fallback = field.spec.Compose.DefaultInput
			display, _ = field.resolve(fallback, values)
		}
		if display == "" {
			fallback = ""
		}
	} else if !validInput(field.spec.Kind, prior) {
		fallback, display = "", ""
	}
	label := field.spec.Label
	if field.spec.Kind == "dnsProvider" {
		label += " (" + dnsprovider.Choices() + ")"
		display = field.display(display)
	}
	if field.spec.Help != "" {
		wizard(ctx).note(field.spec.Help)
	}
	for {
		raw, err := ask(label, fallback, display)
		if err != nil {
			return "", "", err
		}
		resolved, err := field.resolve(raw, values)
		if err == nil {
			if field.spec.Compose == nil {
				raw = resolved
			}
			return raw, resolved, nil
		}
		wizard(ctx).note(err.Error())
	}
}

func (inputs featureInputs) save(file, dir string) error {
	if len(inputs) == 0 {
		return nil
	}
	out := map[string]any{"Version": 1}
	rawInputs := make(map[string]string)
	for _, field := range inputs {
		out[field.spec.Name] = field.value
		if field.spec.Compose != nil && field.acceptsInput() {
			rawInputs[field.spec.Name] = field.raw
		}
	}
	if len(rawInputs) > 0 {
		out["ComposeInputs"] = rawInputs
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, file), raw)
}

func inputKind(kind string) bool {
	return kind == "email" || kind == "domain" || kind == "text" || kind == "dnsProvider"
}

var domainLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func validInput(kind, value string) bool {
	switch kind {
	case "email":
		address, err := mail.ParseAddress(value)
		return err == nil && address.Address == value
	case "domain":
		if len(value) > 253 {
			return false
		}
		for _, label := range strings.Split(value, ".") {
			if !domainLabel.MatchString(label) {
				return false
			}
		}
		return true
	case "dnsProvider":
		_, ok := dnsprovider.Lookup(value)
		return ok
	case "text":
		return len(value) > 0 && len(value) <= 4096 && utf8.ValidString(value) &&
			strings.IndexFunc(value, unicode.IsControl) < 0
	default:
		return false
	}
}

func (field featureInput) display(value string) string {
	if field.spec.Kind == "dnsProvider" {
		if provider, ok := dnsprovider.Lookup(value); ok {
			return provider.Name
		}
	}
	return value
}

func (inputs featureInputs) showDNSGuidance(ctx context.Context) {
	for _, field := range inputs {
		if field.spec.Kind == "dnsProvider" {
			if provider, ok := dnsprovider.Lookup(field.value); ok {
				wizard(ctx).note(provider.Guidance)
			}
		}
	}
}

func (inputs featureInputs) githubVariables() map[string]string {
	result := make(map[string]string, len(inputs))
	for _, field := range inputs {
		if field.spec.GitHubVariable != "" {
			result[field.spec.GitHubVariable] = field.raw
		}
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
