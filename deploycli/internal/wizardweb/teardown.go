package wizardweb

import "context"

// TeardownTarget names the verified scope in every confirmation step.
type TeardownTarget struct {
	Title   string   `json:"title"`
	Name    string   `json:"name"`
	Targets []string `json:"targets"`
	Effects []string `json:"effects"`
	Intent  string   `json:"intent"`
	Final   string   `json:"final"`
	Type    string   `json:"type"`
}

type TeardownPrompt struct {
	TeardownTarget
	Step int `json:"step"`
}

func (p TeardownPrompt) expectedAnswer() string {
	if p.Step == 3 {
		return p.Type
	}
	return "yes"
}

// ConfirmTeardown gives each step its own prompt ID and waits for an explicit
// answer before publishing the next. Cancellation never grants approval.
func (s *Session) ConfirmTeardown(ctx context.Context, target TeardownTarget) error {
	for step := 1; step <= 3; step++ {
		if _, err := s.Ask(ctx, Prompt{
			Kind: "teardown", Label: target.Title,
			Teardown: &TeardownPrompt{TeardownTarget: target, Step: step},
		}); err != nil {
			return err
		}
	}
	return nil
}
