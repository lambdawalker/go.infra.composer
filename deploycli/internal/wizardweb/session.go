// Package wizardweb presents a single local deployment wizard in a browser.
package wizardweb

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/lambdawalker/go.infra.composer/reporting"
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
	Saved    bool     `json:"saved,omitempty"`
	Options  []Option `json:"options,omitempty"`
	Action   *Option  `json:"action,omitempty"`
	ReadOnly bool     `json:"readOnly,omitempty"`
	Composed bool     `json:"composed,omitempty"`
	Help     string   `json:"help,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type Prompt struct {
	ID               uint64                              `json:"id"`
	Kind             string                              `json:"kind"`
	Label            string                              `json:"label"`
	Default          string                              `json:"default,omitempty"`
	CountdownSeconds int                                 `json:"countdownSeconds,omitempty"`
	Message          string                              `json:"message,omitempty"`
	Options          []Option                            `json:"options,omitempty"`
	Fields           []Field                             `json:"fields,omitempty"`
	PreviewFields    []string                            `json:"previewFields,omitempty"`
	Preview          func(map[string]string) FormPreview `json:"-"`
}

type FormPreview struct {
	Values map[string]string `json:"values,omitempty"`
	Errors map[string]string `json:"errors,omitempty"`
}

type Change struct {
	Resource  string `json:"resource"`
	Operation string `json:"operation"`
}

// ArtifactView contains only the safe identity and relationship fields used by
// the workspace. Provider configuration and credentials never enter the page.
type ArtifactView struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Parent   string `json:"parent,omitempty"`
	Owner    string `json:"owner,omitempty"`
	Feature  string `json:"feature,omitempty"`
	Action   string `json:"action,omitempty"`
	Presence string `json:"presence"`
}

type InventoryView struct {
	Repository  string         `json:"repository,omitempty"`
	Environment string         `json:"environment,omitempty"`
	Account     string         `json:"account,omitempty"`
	Region      string         `json:"region,omitempty"`
	Project     string         `json:"project,omitempty"`
	Backend     string         `json:"backend,omitempty"`
	Stack       string         `json:"stack,omitempty"`
	Revision    string         `json:"revision,omitempty"`
	Artifacts   []ArtifactView `json:"artifacts"`
}

type view struct {
	Locked      bool           `json:"locked,omitempty"`
	Run         uint64         `json:"run"`
	Revision    uint64         `json:"revision"`
	Title       string         `json:"title"`
	Action      string         `json:"action,omitempty"`
	Phase       string         `json:"phase"`
	PhaseTitle  string         `json:"phaseTitle"`
	Visited     []string       `json:"visited,omitempty"`
	Status      string         `json:"status"`
	Error       string         `json:"error,omitempty"`
	Diagnostics string         `json:"diagnostics,omitempty"`
	Prompt      *Prompt        `json:"prompt,omitempty"`
	Activity    []string       `json:"activity"`
	SignIn      string         `json:"signIn,omitempty"`
	Changes     []Change       `json:"changes,omitempty"`
	Total       int            `json:"total"`
	Reviewed    bool           `json:"reviewed"`
	Inventory   *InventoryView `json:"inventory,omitempty"`
}

type reply struct {
	Value  *string           `json:"value"`
	Values map[string]string `json:"values"`
	Action string            `json:"action,omitempty"`
}

type Session struct {
	vault           vaultSession
	mu              sync.Mutex
	view            view
	response        chan reply
	cancel          context.CancelFunc
	close           context.CancelFunc
	secrets         []string
	commandFailures []string
	home            chan struct{}
}

func newSession(title string, close context.CancelFunc) *Session {
	return &Session{
		view:  view{Run: 1, Revision: 1, Title: title, Phase: "settings", PhaseTitle: "Getting ready", Status: "running"},
		close: close,
	}
}

func (s *Session) startOperation(lifetime context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(lifetime)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = cancel
	if s.view.Status == "stopping" {
		cancel()
	}
	return ctx, cancel
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
	prompt.Fields = slices.Clone(prompt.Fields)
	prompt.PreviewFields = slices.Clone(prompt.PreviewFields)
	for i := range prompt.Fields {
		field := &prompt.Fields[i]
		if field.Kind == "password" {
			field.Value = ""
			prompt.PreviewFields = slices.DeleteFunc(prompt.PreviewFields, func(name string) bool {
				return name == field.Name
			})
		}
	}
	answer, err := s.ask(ctx, prompt)
	return answer.Values, answer.Action, err
}

func (s *Session) ask(ctx context.Context, prompt Prompt) (reply, error) {
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return reply{}, err
	}
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
	if s.vaultLocked() || prompt == nil || prompt.ID != id || s.view.Status != "running" {
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
		for _, field := range prompt.Fields {
			if field.Kind == "password" {
				s.protect(answer.Values[field.Name])
			}
		}
	} else if answer.Value == nil || answer.Values != nil || answer.Action != "" {
		return false
	} else if prompt.Kind == "secret" {
		s.protect(*answer.Value)
	}
	s.response <- answer
	s.response = nil
	s.view.Prompt = nil
	s.view.Revision++
	return true
}

// preview evaluates an immutable draft without advancing the current prompt.
func (s *Session) preview(id uint64, values map[string]string) (FormPreview, bool) {
	s.mu.Lock()
	prompt := s.view.Prompt
	if s.vaultLocked() || prompt == nil || prompt.ID != id || prompt.Kind != "form" ||
		prompt.Preview == nil || s.view.Status != "running" || len(values) != len(prompt.PreviewFields) {
		s.mu.Unlock()
		return FormPreview{}, false
	}
	for _, field := range prompt.PreviewFields {
		if _, ok := values[field]; !ok {
			s.mu.Unlock()
			return FormPreview{}, false
		}
	}
	compute := prompt.Preview
	s.mu.Unlock()
	return compute(values), true
}

func (s *Session) redact(text string) string {
	if passphrase := s.vault.passphrase; passphrase != "" {
		text = strings.ReplaceAll(text, passphrase, "[redacted]")
	}
	for _, secret := range s.secrets {
		text = strings.ReplaceAll(text, secret, "[redacted]")
	}
	return text
}

func (s *Session) Protect(values ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.protect(values...)
}

// protect adds redactions while the caller holds the session mutex.
func (s *Session) protect(values ...string) {
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

func (s *Session) Inventory(report reporting.Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]ArtifactView, 0, len(report.Artifacts))
	for _, a := range report.Artifacts {
		items = append(items, ArtifactView{
			Key: s.redact(a.Key), Name: s.redact(a.Name), Type: a.Type,
			Parent: s.redact(a.Parent), Owner: s.redact(a.Owner), Feature: s.redact(a.Feature),
			Action: a.PlannedAction, Presence: string(a.Observation.Status),
		})
	}
	s.view.Inventory = &InventoryView{
		Repository: s.redact(report.Scope.Repository), Environment: report.Scope.Environment,
		Account: report.Scope.Account, Region: report.Scope.Region,
		Project: report.Scope.Project, Backend: s.redact(report.Scope.Backend),
		Stack: report.Scope.Stack, Revision: report.Metadata.Revision, Artifacts: items,
	}
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
	if s.vaultLocked() {
		return view{Revision: s.view.Revision, Locked: true}
	}
	v := s.view
	v.Activity = slices.Clone(v.Activity)
	v.Visited = slices.Clone(v.Visited)
	return v
}

func (s *Session) finish(err error) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancelled := s.view.Status == "stopping" || errors.Is(err, context.Canceled)
	s.view.Status = "complete"
	switch {
	case cancelled:
		s.view.Status = "cancelled"
	case err != nil:
		s.view.Status = "failed"
		s.view.Error = s.sanitizeDiagnostic(err.Error())
	}
	if s.view.Status != "complete" {
		s.view.Diagnostics = s.failureDiagnostics()
	}
	s.commandFailures = nil
	clear(s.secrets)
	s.secrets = nil
	s.view.Prompt, s.response, s.cancel = nil, nil, nil
	s.view.SignIn = ""
	s.view.Revision++
	s.home = make(chan struct{})
	return s.home
}

func (s *Session) returnToActions(revision uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vaultLocked() || s.home == nil || s.view.Revision != revision {
		return false
	}
	s.view = view{
		Run: s.view.Run + 1, Revision: revision + 1, Title: s.view.Title,
		Phase: "settings", PhaseTitle: "Getting ready", Status: "running",
	}
	close(s.home)
	s.home = nil
	return true
}

// stop handles the intent and run the browser displayed when the button was clicked.
func (s *Session) stop(run uint64, closePage bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vaultLocked() || run != s.view.Run {
		return false
	}
	if closePage {
		atMenu := s.view.Prompt != nil && s.view.Prompt.Kind == "actions"
		if s.home == nil && !atMenu {
			return false
		}
		s.home = nil
		s.close()
		return true
	}
	switch s.view.Status {
	case "running":
		s.view.Status = "stopping"
		s.view.Prompt = nil
		s.view.Revision++
		if s.cancel != nil {
			s.cancel()
		}
		return true
	case "stopping":
		return true
	}
	return false
}
