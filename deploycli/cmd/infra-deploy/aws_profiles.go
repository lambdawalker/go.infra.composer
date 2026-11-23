package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

const configureSSO = "configure-sso"

func listAWSProfiles(ctx context.Context) ([]wizardweb.Option, error) {
	raw, err := output(ctx, "aws", "configure", "list-profiles")
	if err != nil {
		return nil, fmt.Errorf("list AWS profiles; install AWS CLI v2 and run aws configure sso: %w", err)
	}
	var profiles []wizardweb.Option
	for _, name := range strings.Fields(string(raw)) {
		profiles = append(profiles, wizardweb.Option{Value: name, Label: name})
	}
	return profiles, nil
}

func defaultAWSProfile(profiles []wizardweb.Option) string {
	first := ""
	for _, profile := range profiles {
		if profile.Value == "default" {
			return profile.Value
		}
		if first == "" {
			first = profile.Value
		}
	}
	return first
}

func profileSetupTerminal() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

func (f deploymentForm) profileField() wizardweb.Field {
	field := wizardweb.Field{Name: "profile", Group: "AWS", Label: "AWS SSO profile",
		Value: f.target.Profile, Kind: "select", Options: f.profiles}
	if f.canConfigureSSO {
		field.Action = &wizardweb.Option{Value: configureSSO, Label: "Add AWS SSO profile"}
		field.Help = "The AWS CLI will guide you through setup in the terminal where you started the wizard."
	} else {
		field.Action = &wizardweb.Option{Value: "refresh-profiles", Label: "Refresh AWS profiles"}
		field.Help = "To add a profile, run aws configure sso in a terminal, then refresh this list."
	}
	return field
}

func (f *deploymentForm) refreshProfiles(ctx context.Context, action string, draft map[string]string) error {
	ui := wizard(ctx)
	var setupErr error
	if action == configureSSO {
		ui.phase("settings", "Finish AWS SSO setup in the terminal")
		ui.note("Complete the AWS CLI prompts in the terminal. The settings form will reopen when setup ends.")
		setupErr = command(ctx, f.root, "aws", "configure", "sso")
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	profiles, err := listAWSProfiles(ctx)
	if err != nil {
		return err
	}
	draft["profile"] = selectRefreshedProfile(draft["profile"], f.profiles, profiles)
	f.profiles = profiles
	if setupErr != nil {
		return fmt.Errorf("AWS SSO setup did not complete. Check the terminal to see what happened")
	}
	ui.note("AWS profiles refreshed.")
	return nil
}

func selectRefreshedProfile(selected string, old, current []wizardweb.Option) string {
	var added []string
	for _, profile := range current {
		if !hasAWSProfile(old, profile.Value) {
			added = append(added, profile.Value)
		}
	}
	if len(added) == 1 {
		return added[0]
	}
	if hasAWSProfile(current, selected) {
		return selected
	}
	return defaultAWSProfile(current)
}

func hasAWSProfile(profiles []wizardweb.Option, name string) bool {
	return slices.ContainsFunc(profiles, func(option wizardweb.Option) bool { return option.Value == name })
}
