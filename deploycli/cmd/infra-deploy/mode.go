package main

import (
	"context"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

func chooseWizardMode(ctx context.Context) (bool, error) {
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) || !wizardweb.DesktopAvailable(ctx) {
		return false, nil
	}
	for {
		answer, err := askInput("Continue in Terminal (1) or launch the Web interface (2)?", "terminal", "Terminal")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "1", "terminal":
			return false, nil
		case "2", "web":
			return true, nil
		default:
			note("Choose 1 for Terminal or 2 for Web.")
		}
	}
}
