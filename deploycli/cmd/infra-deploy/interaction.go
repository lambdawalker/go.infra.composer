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
	answer, err := w.browser.Ask(w.ctx, wizardweb.Prompt{Kind: "text", Label: label, Default: display})
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

func (w interaction) selectAction(label, def string, options []wizardweb.Option) (string, error) {
	for {
		value, err := w.browser.Ask(w.ctx, wizardweb.Prompt{Kind: "actions", Label: label, Default: def, Options: options})
		if err != nil {
			return "", err
		}
		for _, option := range options {
			if option.Value == value {
				return value, nil
			}
		}
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
