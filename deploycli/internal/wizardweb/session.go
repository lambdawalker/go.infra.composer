// Package wizardweb presents a single local deployment wizard in a browser.
package wizardweb

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

type Option struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Field struct {
	Name     string   `json:"name"`
	Group    string   `json:"group"`
	Label    string   `json:"label"`
	Value    string   `json:"value"`
	Kind     string   `json:"kind"`
	Options  []Option `json:"options,omitempty"`
	Action   *Option  `json:"action,omitempty"`
	ReadOnly bool     `json:"readOnly,omitempty"`
	Help     string   `json:"help,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type Prompt struct {
	ID      uint64   `json:"id"`
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Default string   `json:"default,omitempty"`
	Message string   `json:"message,omitempty"`
	Options []Option `json:"options,omitempty"`
	Fields  []Field  `json:"fields,omitempty"`
}

type Change struct {
	Resource  string `json:"resource"`
	Operation string `json:"operation"`
}

type view struct {
	Revision   uint64   `json:"revision"`
	Title      string   `json:"title"`
	Action     string   `json:"action,omitempty"`
	Phase      string   `json:"phase"`
	PhaseTitle string   `json:"phaseTitle"`
	Visited    []string `json:"visited,omitempty"`
	Status     string   `json:"status"`
	Error      string   `json:"error,omitempty"`
	Prompt     *Prompt  `json:"prompt,omitempty"`
	Activity   []string `json:"activity"`
	SignIn     string   `json:"signIn,omitempty"`
	Changes    []Change `json:"changes,omitempty"`
	Total      int      `json:"total"`
	Reviewed   bool     `json:"reviewed"`
}

type reply struct {
	Value  *string           `json:"value"`
	Values map[string]string `json:"values"`
	Action string            `json:"action,omitempty"`
}

type Session struct {
	mu       sync.Mutex
	view     view
	response chan reply
	cancel   context.CancelFunc
	close    context.CancelFunc
	secrets  []string
}

func newSession(title string, cancel, close context.CancelFunc) *Session {
	return &Session{
		view:   view{Revision: 1, Title: title, Phase: "settings", PhaseTitle: "Getting ready", Status: "running"},
		cancel: cancel, close: close,
	}
}

func (s *Session) Ask(ctx context.Context, prompt Prompt) (string, error) {
	answer, err := s.ask(ctx, prompt)
	if answer.Value == nil {
		return "", err
	}
	return *answer.Value, err
}

func (s *Session) AskForm(ctx context.Context, prompt Prompt) (map[string]string, string, error) {
	prompt.Kind = "form"
	answer, err := s.ask(ctx, prompt)
	return answer.Values, answer.Action, err
}

func (s *Session) ask(ctx context.Context, prompt Prompt) (reply, error) {
	s.mu.Lock()
	s.view.Revision++
	prompt.ID = s.view.Revision
	s.view.Prompt = &prompt
	response := make(chan reply, 1)
	s.response = response
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		return reply{}, ctx.Err()
	case value := <-response:
		return value, ctx.Err()
	}
}

func (s *Session) answer(id uint64, answer reply) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	prompt := s.view.Prompt
	if prompt == nil || prompt.ID != id || s.view.Status != "running" {
		return false
	}
	if prompt.Kind == "form" {
		count := 0
		actionAvailable := answer.Action == ""
		for _, field := range prompt.Fields {
			if field.Action != nil && field.Action.Value == answer.Action {
				actionAvailable = true
			}
			if field.ReadOnly {
				continue
			}
			if _, ok := answer.Values[field.Name]; !ok {
				return false
			}
			count++
		}
		if answer.Value != nil || answer.Values == nil || len(answer.Values) != count || !actionAvailable {
			return false
		}
	} else if answer.Value == nil || answer.Values != nil || answer.Action != "" {
		return false
	} else if value := *answer.Value; prompt.Kind == "secret" && value != "" && !slices.Contains(s.secrets, value) {
		s.secrets = append(s.secrets, value)
	}
	s.response <- answer
	s.response = nil
	s.view.Prompt = nil
	s.view.Revision++
	return true
}

func (s *Session) redact(text string) string {
	for _, secret := range s.secrets {
		text = strings.ReplaceAll(text, secret, "[redacted]")
	}
	return text
}

func (s *Session) Protect(values ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, value := range values {
		if value != "" && !slices.Contains(s.secrets, value) {
			s.secrets = append(s.secrets, value)
		}
	}
}

func (s *Session) Note(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text = s.redact(strings.TrimSpace(text))
	if text == "" {
		return
	}
	if len(text) > 4096 {
		text = text[:4096] + "…"
	}
	if len(s.view.Activity) == 120 {
		s.view.Activity = slices.Delete(s.view.Activity, 0, 1)
	}
	s.view.Activity = append(s.view.Activity, text)
	s.view.Revision++
}

func (s *Session) Phase(phase, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Contains(s.view.Visited, phase) {
		s.view.Visited = append(s.view.Visited, phase)
	}
	s.view.Phase, s.view.PhaseTitle = phase, title
	s.view.Revision++
}

func (s *Session) Action(action string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view.Action = action
	s.view.Revision++
}

func (s *Session) Preview(changes []Change, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range changes {
		changes[i].Resource = s.redact(changes[i].Resource)
		changes[i].Operation = s.redact(changes[i].Operation)
	}
	s.view.Changes, s.view.Total, s.view.Reviewed = changes, total, true
	s.view.Revision++
}

func (s *Session) SignInWriter() *signInOutput { return &signInOutput{session: s} }

type signInOutput struct {
	session *Session
	pending string
}

func (w *signInOutput) Write(p []byte) (int, error) {
	s := w.session
	s.mu.Lock()
	defer s.mu.Unlock()
	if remaining := 4096 - len(s.view.SignIn) - len(w.pending); remaining > 0 {
		w.pending += string(p[:min(len(p), remaining)])
		// Publish whole lines so a secret split across multiple writes is redacted.
		if end := strings.LastIndexByte(w.pending, '\n'); end >= 0 {
			s.view.SignIn += s.redact(w.pending[:end+1])
			w.pending = w.pending[end+1:]
			s.view.Revision++
		}
	}
	return len(p), nil
}

func (s *Session) ClearSignIn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view.SignIn = ""
	s.view.Revision++
}

func (s *Session) snapshot() view {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.view
	v.Activity = slices.Clone(v.Activity)
	v.Visited = slices.Clone(v.Visited)
	return v
}

func (s *Session) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancelled := s.view.Status == "stopping" || errors.Is(err, context.Canceled)
	s.view.Status = "complete"
	switch {
	case cancelled:
		s.view.Status = "cancelled"
	case err != nil:
		s.view.Status = "failed"
		s.view.Error = s.redact(fmt.Sprint(err))
	}
	clear(s.secrets)
	s.secrets = nil
	s.view.Prompt, s.response = nil, nil
	s.view.SignIn = ""
	s.view.Revision++
}

func (s *Session) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.view.Status {
	case "running":
		s.view.Status = "stopping"
		s.view.Prompt = nil
		s.view.Revision++
		s.cancel()
	case "complete", "failed", "cancelled":
		s.close()
	}
}
