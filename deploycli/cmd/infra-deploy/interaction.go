package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

type wizardKey struct{}

type interaction struct {
	ctx     context.Context
	browser *wizardweb.Session
}

func wizard(ctx context.Context) interaction {
	browser, _ := ctx.Value(wizardKey{}).(*wizardweb.Session)
	return interaction{ctx: ctx, browser: browser}
}

func (w interaction) phase(phase, title string) {
	if w.browser != nil {
		w.browser.Phase(phase, title)
	}
}

func (w interaction) heading(title string) {
	heading(title)
	if w.browser != nil {
		w.browser.Note(title)
	}
}

func (w interaction) detail(label, value string) {
	detail(label, value)
	if w.browser != nil {
		w.browser.Note(label + "  " + value)
	}
}

func (w interaction) note(value string) {
	note(value)
	if w.browser != nil {
		w.browser.Note(value)
	}
}

func (w interaction) result(text string) {
	fmt.Println(text)
	if w.browser != nil {
		w.browser.Note(text)
	}
}

func (w interaction) input(label, fallback, display string) (string, error) {
	if w.browser == nil {
		return askInput(label, fallback, display)
	}
	prompt := wizardweb.Prompt{Kind: "text", Label: label, Default: display}
	if fallback != "" {
		prompt.CountdownSeconds = defaultCountdownSeconds
	}
	answer, err := w.browser.Ask(w.ctx, prompt)
	if strings.TrimSpace(answer) == "" {
		answer = fallback
	}
	return strings.TrimSpace(answer), err
}

func (w interaction) prompt(label, fallback string) string {
	value, err := w.input(label, fallback, fallback)
	if err != nil {
		panic(err)
	}
	return value
}

func (w interaction) secret(label string) (string, error) {
	if w.browser == nil {
		return secret(label)
	}
	value, err := w.browser.Ask(w.ctx, wizardweb.Prompt{Kind: "secret", Label: label})
	value = strings.TrimSpace(value)
	w.browser.Protect(value)
	return value, err
}

func (w interaction) choose(label, approveLabel string, def bool, details ...string) (bool, error) {
	if w.browser == nil {
		heading(label)
		return timedChoice(def)
	}
	label = strings.TrimSuffix(strings.TrimSuffix(label, " [Y/n]"), " [y/N]")
	value, err := w.browser.Ask(w.ctx, wizardweb.Prompt{Kind: "confirm", Label: label, Message: strings.Join(details, " · "), Options: []wizardweb.Option{
		{Value: "no", Label: "No"},
		{Value: "yes", Label: approveLabel},
	}})
	return value == "yes", err
}

func (w interaction) declined(message string) error {
	if w.browser != nil {
		return fmt.Errorf("%s: %w", message, context.Canceled)
	}
	return errors.New(message)
}

func (w interaction) deliberate(label, expected string) error {
	if w.browser == nil {
		return deliberate(label, expected)
	}
	value, err := w.browser.Ask(w.ctx, wizardweb.Prompt{Kind: "deliberate", Label: label, Message: "Type " + expected + " to confirm."})
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) != expected {
		return fmt.Errorf("explicit confirmation did not match; cancelled")
	}
	return nil
}

func (w interaction) selectAction(countdown bool) (string, error) {
	options := []wizardweb.Option{
		{Value: "preview", Label: "Preview changes", Description: "Build and see what Pulumi plans to change. The run stops after the preview."},
		{Value: "deploy", Label: "Preview and deploy", Description: "Review the Pulumi plan, then approve it to make changes in AWS."},
		{Value: "setup", Label: "Set up deployment", Description: "Prepare the state bucket, Pulumi stack, and deployment settings."},
		{Value: "status", Label: "Check deployment status", Description: "Read the published outputs from the selected Pulumi stack."},
		{Value: "report", Label: "Generate an artifact report", Description: "Save a report of the deployment artifacts recorded for the selected stack."},
		{Value: "github-setup", Label: "Configure GitHub deployment", Description: "Review and configure the GitHub environment and AWS role used by deployment workflows."},
	}
	prompt := wizardweb.Prompt{Kind: "actions", Label: "What would you like to run?", Default: "preview", Options: options}
	if countdown {
		prompt.CountdownSeconds = defaultCountdownSeconds
	}
	for {
		value, err := w.browser.Ask(w.ctx, prompt)
		if err != nil {
			return "", err
		}
		for _, option := range options {
			if option.Value == value {
				w.browser.Action(value)
				return value, nil
			}
		}
		prompt.CountdownSeconds = 0
	}
}

func (w interaction) login(dir, name string, args ...string) error {
	if w.browser == nil {
		return command(w.ctx, dir, args...)
	}
	w.phase("connect", "Sign in to "+name)
	defer w.browser.ClearSignIn()
	c := exec.CommandContext(w.ctx, args[0], args[1:]...)
	c.Dir = dir
	c.WaitDelay = 2 * time.Second
	writer := w.browser.SignInWriter()
	c.Stdout, c.Stderr = writer, writer
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s sign-in failed: %w", name, err)
	}
	w.note("Signed in to " + name)
	return nil
}
