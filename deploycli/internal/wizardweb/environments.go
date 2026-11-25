package wizardweb

import (
	"errors"
	"slices"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

type Environment struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type EnvironmentTarget struct {
	Inventory reporting.Report
	Source    string
	Fields    []Field
}

var errEnvironmentUnavailable = errors.New("Choose an environment from the action menu.")

func (s *Session) ConfigureEnvironments(selected string, options []Environment, resolve func(string, string) (EnvironmentTarget, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view.Environment = selected
	s.view.Environments = slices.Clone(options)
	s.resolveEnvironment = resolve
	s.view.Revision++
}

func (s *Session) Environment() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view.Environment
}

func (s *Session) selectEnvironment(id uint64, mode, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prompt := s.view.Prompt
	if s.vaultLocked() || s.view.Status != "running" || prompt == nil || prompt.ID != id ||
		(prompt.Kind != "actions" && prompt.Kind != "environment") || s.resolveEnvironment == nil {
		return errEnvironmentUnavailable
	}
	target, err := s.resolveEnvironment(mode, name)
	if err != nil {
		return err
	}
	s.view.Environment = name
	s.setInventory(target.Inventory, target.Source)
	s.setSettingsFields(target.Fields)
	if prompt.Kind == "environment" {
		s.response <- reply{Value: &name}
		s.response = nil
		s.view.Prompt = nil
	} else {
		// An action answer from another tab must refer to the new target.
		next := *prompt
		next.ID = s.view.Revision + 1
		next.CountdownSeconds = 0
		s.view.Prompt = &next
	}
	s.view.Revision++
	return nil
}
