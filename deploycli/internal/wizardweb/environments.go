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

// ProjectTeardownAction is triggered by the project menu, outside the environment action cards.
const ProjectTeardownAction = "destroy-all"

func (s *Session) ConfigureEnvironments(selected string, options []Environment, canRemoveAll bool, resolve func(string, string) (EnvironmentTarget, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openEnvironment(selected)
	s.view.Environments = slices.Clone(options)
	s.view.CanRemoveAll = canRemoveAll
	s.resolveEnvironment = resolve
	s.view.Revision++
}

// openEnvironment tracks tabs in the order they were first opened.
// The caller holds the session mutex.
func (s *Session) openEnvironment(name string) {
	s.view.Environment = name
	if name != "" && !slices.Contains(s.view.OpenEnvironments, name) {
		s.view.OpenEnvironments = append(s.view.OpenEnvironments, name)
	}
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
	onResult := s.home != nil && s.view.Revision == id
	onMenu := s.view.Status == "running" && prompt != nil && prompt.ID == id &&
		(prompt.Kind == "actions" || prompt.Kind == "environment")
	if s.vaultLocked() || (!onMenu && !onResult) || s.resolveEnvironment == nil {
		return errEnvironmentUnavailable
	}
	if mode == "switch" && !slices.Contains(s.view.OpenEnvironments, name) {
		return errors.New("Open this environment from the main menu first.")
	}
	target, err := s.resolveEnvironment(mode, name)
	if err != nil {
		return err
	}
	if onResult {
		s.resetForActions()
	}
	s.openEnvironment(name)
	s.setInventory(target.Inventory, target.Source)
	s.setSettingsFields(target.Fields)
	if onMenu && prompt.Kind == "environment" {
		s.submitReply(reply{Value: &name})
		return nil
	}
	if onMenu {
		// An action answer from another tab must refer to the new target.
		next := *prompt
		next.ID = s.view.Revision + 1
		next.CountdownSeconds = 0
		s.view.Prompt = &next
	}
	s.view.Revision++
	return nil
}

func (s *Session) requestProjectTeardown(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	prompt := s.view.Prompt
	if s.vaultLocked() || !s.view.CanRemoveAll || s.view.Status != "running" ||
		prompt == nil || prompt.Kind != "actions" || prompt.ID != id {
		return false
	}
	action := ProjectTeardownAction
	s.submitReply(reply{Value: &action})
	return true
}
