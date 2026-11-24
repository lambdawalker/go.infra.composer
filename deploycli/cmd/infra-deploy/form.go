package main

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/dnsprovider"
	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

type deploymentForm struct {
	project, action string
	root            string
	canConfigureSSO bool
	target, saved   Settings
	profiles        []wizardweb.Option
	canCreateBucket bool
	memory          int
	hasMemory       bool
	github          *githubOptions
	configureGitHub bool
	inputs          featureInputs
	defaults        map[string]string
}

func (f *deploymentForm) collect(ctx context.Context) error {
	fields := f.fields()
	prompt := wizardweb.Prompt{Label: "Deployment settings"}
	if slices.ContainsFunc(fields, func(field wizardweb.Field) bool { return field.Composed }) {
		inputs := slices.Clone(f.inputs)
		environment := f.target.Environment
		for _, field := range fields {
			if !field.ReadOnly && strings.HasPrefix(field.Name, "feature.") {
				prompt.PreviewFields = append(prompt.PreviewFields, field.Name)
			}
		}
		prompt.Preview = func(values map[string]string) map[string]string {
			draft, _ := resolveFeatureDraft(inputs, environment, values)
			previews := make(map[string]string)
			for _, field := range draft {
				if field.spec.Compose != nil && field.value != "" {
					previews["feature."+field.spec.Name] = field.display(field.value)
				}
			}
			return previews
		}
	}
	message := ""
	for {
		prompt.Message, prompt.Fields = message, fields
		replies, action, err := wizard(ctx).browser.AskForm(ctx, prompt)
		if err != nil {
			return err
		}
		var issues map[string]string
		if action == "" {
			issues = f.apply(replies)
			if len(issues) == 0 {
				return nil
			}
		} else if err := f.refreshProfiles(ctx, action, replies); err != nil {
			issues = map[string]string{"profile": err.Error()}
		}
		message = issues[""]
		fields = slices.Clone(fields)
		for i := range fields {
			field := &fields[i]
			if value, ok := replies[field.Name]; ok {
				field.Value = value
			}
			field.Error = issues[field.Name]
			if field.Name == "profile" {
				field.Options = f.profiles
			}
		}
	}
}

func (f deploymentForm) fields() []wizardweb.Field {
	bound := f.saved.Account != ""
	var accountHelp, backendHelp string
	if !bound {
		accountHelp = "Enter a 12-digit account ID or use auto to resolve it from the profile."
		backendHelp = "Use an existing s3://bucket/prefix."
		if f.canCreateBucket {
			backendHelp += " Enter new to create a state bucket."
		}
	}
	fields := []wizardweb.Field{
		{Name: "project", Group: "Target", Label: "Project", Value: f.project, ReadOnly: true},
		{Name: "environment", Group: "Target", Label: "Environment", Value: f.target.Environment, ReadOnly: true},
		f.profileField(),
		{Name: "region", Group: "AWS", Label: "AWS region", Value: f.target.Region, ReadOnly: bound},
		{Name: "account", Group: "AWS", Label: "AWS account", Value: f.target.Account, ReadOnly: bound,
			Help: accountHelp},
		{Name: "backend", Group: "AWS", Label: "S3 state URL", Value: f.target.Backend, ReadOnly: bound, Help: backendHelp},
	}
	if f.hasMemory {
		fields = append(fields, wizardweb.Field{Name: "memory", Group: "AWS", Label: "Lambda memory (MB)", Value: strconv.Itoa(f.memory), Kind: "number", Help: "128–10,240 MB."})
	}
	for _, input := range f.inputs {
		name := input.spec.Name
		value := f.defaults[name]
		kind := input.spec.Kind
		if input.flag != nil && *input.flag != "" {
			value = *input.flag
		}
		if input.spec.Compose != nil {
			kind = input.userKind
			if value == "" {
				value = input.spec.Compose.DefaultInput
			}
		}
		field := wizardweb.Field{
			Name: "feature." + name, Group: "Project inputs", Label: input.spec.Label,
			Kind: kind, Value: value, Help: input.spec.Help,
			Composed: input.spec.Compose != nil, ReadOnly: input.flag == nil,
		}
		if kind == "dnsProvider" && !field.ReadOnly {
			for _, provider := range dnsprovider.All() {
				field.Options = append(field.Options, wizardweb.Option{Value: provider.ID, Label: provider.Name})
			}
			if p, ok := dnsprovider.Lookup(value); ok {
				field.Value = p.ID
			}
		}
		fields = append(fields, field)
	}
	if f.github != nil {
		fields = append(fields, f.github.formFields(f.action == "github-setup", f.configureGitHub)...)
	}
	return fields
}

func (f *deploymentForm) apply(values map[string]string) map[string]string {
	for name, value := range values {
		values[name] = strings.TrimSpace(value)
	}
	issues := map[string]string{}
	target := f.target
	target.Profile = values["profile"]
	if !hasAWSProfile(f.profiles, target.Profile) {
		issues["profile"] = "Select an AWS profile."
	}
	if f.saved.Account == "" {
		target.Region, target.Account, target.Backend = values["region"], values["account"], values["backend"]
		if !validRegion(target.Region) {
			issues["region"] = "Enter an AWS region such as us-east-2."
		}
		if target.Account == "auto" {
			target.Account = ""
		} else if !pattern(`^\d{12}$`, target.Account) {
			issues["account"] = "Enter a 12-digit AWS account ID or auto."
		}
		if !validBackend(target.Backend) && !(f.canCreateBucket && target.Backend == "new") {
			issues["backend"] = "Enter a valid S3 URL with a bucket and prefix."
		}
	}

	memory := f.memory
	if f.hasMemory {
		var err error
		memory, err = strconv.Atoi(values["memory"])
		if err != nil || memory < 128 || memory > 10240 {
			issues["memory"] = "Enter a whole number from 128 to 10,240."
		}
	}

	draft, inputIssues := resolveFeatureDraft(f.inputs, f.target.Environment, values)
	maps.Copy(issues, inputIssues)

	reconcile := f.github != nil && (f.action == "github-setup" || values["github.enabled"] == "yes")
	var github githubOptions
	if reconcile {
		github = *f.github
		github.readForm(values, issues)
	}
	if len(issues) == 0 {
		f.target, f.memory, f.configureGitHub = target, memory, reconcile
		copy(f.inputs, draft)
		if reconcile {
			*f.github = github
		}
	}
	return issues
}

func resolveFeatureDraft(inputs featureInputs, environment string, values map[string]string) (featureInputs, map[string]string) {
	draft := slices.Clone(inputs)
	issues := make(map[string]string)
	resolved := make(map[string]string, len(draft))
	for i := range draft {
		field := &draft[i]
		name := field.spec.Name
		key := "feature." + name
		raw := strings.TrimSpace(values[key])
		field.raw, field.value = "", ""
		missingDependency := ""
		if compose := field.spec.Compose; compose != nil {
			for _, binding := range compose.Parameters {
				if binding.Source == "field" && resolved[binding.Name] == "" {
					missingDependency = binding.Name
					break
				}
			}
		}
		if missingDependency != "" {
			if field.flag != nil {
				issues[key] = fmt.Sprintf("Check the value for %s first.", missingDependency)
			}
			continue
		}
		value, err := field.resolve(raw, environment, resolved)
		if err != nil {
			if field.flag == nil {
				// A derived value has no control to attach the error to.
				issues[""] = err.Error()
			} else {
				issues[key] = err.Error()
			}
			continue
		}
		if field.spec.Compose == nil {
			raw = value
		}
		field.raw, field.value = raw, value
		resolved[name] = value
	}

	return draft, issues
}
