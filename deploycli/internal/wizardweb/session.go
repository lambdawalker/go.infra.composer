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
	Teardown         *TeardownPrompt                     `json:"teardown,omitempty"`
}

type FormPreview struct {
	Values map[string]string `json:"values,omitempty"`
	Errors map[string]string `json:"errors,omitempty"`
}

type Change struct {
	Resource          string           `json:"resource"`
	Operation         string           `json:"operation"`
	Feature           string           `json:"feature,omitempty"`
	Properties        []PropertyChange `json:"properties,omitempty"`
	ReplacementFields []string         `json:"replacementFields,omitempty"`
}

type PropertyChange struct {
	Name   string `json:"name"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// ArtifactView contains only the safe identity and relationship fields used by
// the workspace. Provider configuration and credentials never enter the page.
type ArtifactView struct {
	Attachment   string   `json:"attachment,omitempty"`
	IconVariant  string   `json:"iconVariant,omitempty"`
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Description  string   `json:"description,omitempty"`
	Parent       string   `json:"parent,omitempty"`
	Component    bool     `json:"component,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Owner        string   `json:"owner,omitempty"`
	Feature      string   `json:"feature,omitempty"`
	Action       string   `json:"action,omitempty"`
	Presence     string   `json:"presence"`
}

type InventoryView struct {
	Source      string         `json:"source,omitempty"`
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

// Substep records a stage within a deployment phase for the progress tree.
// Titles are short, redacted status text; process output stays in Activity.
type Substep struct {
	Phase  string `json:"phase"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type view struct {
	CanCheckDrift       bool                     `json:"canCheckDrift,omitempty"`
	Drift               *DriftView               `json:"drift,omitempty"`
	Locked              bool                     `json:"locked,omitempty"`
	Run                 uint64                   `json:"run"`
	Revision            uint64                   `json:"revision"`
	Title               string                   `json:"title"`
	Action              string                   `json:"action,omitempty"`
	Phase               string                   `json:"phase"`
	PhaseTitle          string                   `json:"phaseTitle"`
	Visited             []string                 `json:"visited,omitempty"`
	Substeps            []Substep                `json:"substeps,omitempty"`
	Status              string                   `json:"status"`
	Error               string                   `json:"error,omitempty"`
	Diagnostics         string                   `json:"diagnostics,omitempty"`
	Prompt              *Prompt                  `json:"prompt,omitempty"`
	SettingsFields      []Field                  `json:"settingsFields,omitempty"`
	ActionOptions       []Option                 `json:"actionOptions,omitempty"`
	ActionDefault       string                   `json:"actionDefault,omitempty"`
	Environment         string                   `json:"environment,omitempty"`
	DeploymentHistory   []DeploymentHistoryEntry `json:"deploymentHistory,omitempty"`
	Environments        []Environment            `json:"environments,omitempty"`
	OpenEnvironments    []string                 `json:"openEnvironments,omitempty"`
	CanRemoveAll        bool                     `json:"canRemoveAll,omitempty"`
	Activity            []string                 `json:"activity"`
	SignIn              string                   `json:"signIn,omitempty"`
	Changes             []Change                 `json:"changes,omitempty"`
	ChangeCountsByOwner map[string]int           `json:"changeCountsByOwner,omitempty"`
	Total               int                      `json:"total"`
	Reviewed            bool                     `json:"reviewed"`
	Inventory           *InventoryView           `json:"inventory,omitempty"`
}

type reply struct {
	Value  *string           `json:"value"`
	Values map[string]string `json:"values"`
	Action string            `json:"action,omitempty"`
}

type Session struct {
	artifactAttachments  map[string]string
	artifactIconVariants map[string]string
	pendingDriftRun      uint64
	instanceID           string
	repository           string
	vault                vaultSession
	mu                   sync.Mutex
	view                 view
	response             chan reply
	cancel               context.CancelFunc
	close                context.CancelFunc
	secrets              []string
	commandFailures      []string
	home                 chan struct{}
	resolveEnvironment   func(string, string) (EnvironmentTarget, error)
	history              []DeploymentHistoryRecord
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

func cloneFields(fields []Field) []Field {
	copy := slices.Clone(fields)
	for i := range copy {
		copy[i].Options = slices.Clone(copy[i].Options)
		if copy[i].Action != nil {
			action := *copy[i].Action
			copy[i].Action = &action
		}
	}
	return copy
}

// SetSettingsFields publishes only a safe, editable-later snapshot. Password
// contents never enter the persistent view or a browser response.
func (s *Session) SetSettingsFields(fields []Field) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setSettingsFields(fields)
	s.view.Revision++
}

func (s *Session) setSettingsFields(fields []Field) {
	copy := cloneFields(fields)
	for i := range copy {
		field := &copy[i]
		if field.Kind == "password" {
			field.Value = ""
		} else {
			field.Value = s.redact(field.Value)
		}
		field.Help = s.redact(field.Help)
		field.Label = s.redact(field.Label)
		field.Error = ""
		for j := range field.Options {
			field.Options[j].Value = s.redact(field.Options[j].Value)
			field.Options[j].Label = s.redact(field.Options[j].Label)
			field.Options[j].Description = s.redact(field.Options[j].Description)
		}
		if field.Action != nil {
			field.Action.Value = s.redact(field.Action.Value)
			field.Action.Label = s.redact(field.Action.Label)
			field.Action.Description = s.redact(field.Action.Description)
		}
	}
	s.view.SettingsFields = copy
}

// SetPasswordSaved updates saved-credential markers after the vault opens.
// The decrypted values themselves never enter the persistent browser view.
func (s *Session) SetPasswordSaved(saved map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.view.SettingsFields {
		field := &s.view.SettingsFields[i]
		if field.Kind == "password" {
			field.Value = ""
			field.Saved = saved[field.Name]
		}
	}
	s.view.Revision++
}

// SetArtifactAttachments provides project-declared service attachments for saved reports.
func (s *Session) SetArtifactAttachments(attachments map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifactAttachments = attachments
}

// SetArtifactIconVariants selects resource-specific artwork for this manifest.
func (s *Session) SetArtifactIconVariants(variants map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifactIconVariants = variants
}

func (s *Session) SetActionOptions(options []Option, fallback string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view.ActionOptions = slices.Clone(options)
	for i := range s.view.ActionOptions {
		s.view.ActionOptions[i].Value = s.redact(s.view.ActionOptions[i].Value)
		s.view.ActionOptions[i].Label = s.redact(s.view.ActionOptions[i].Label)
		s.view.ActionOptions[i].Description = s.redact(s.view.ActionOptions[i].Description)
	}
	s.view.ActionDefault = fallback
	s.view.Revision++
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
	s.SetSettingsFields(prompt.Fields)
	answer, err := s.ask(ctx, prompt)
	return answer.Values, answer.Action, err
}

func (s *Session) ask(ctx context.Context, prompt Prompt) (reply, error) {
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return reply{}, err
	}
	if prompt.Kind == "actions" && s.pendingDriftRun == s.view.Run {
		s.pendingDriftRun = 0
		s.mu.Unlock()
		action := DriftAction
		return reply{Value: &action}, nil
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
	if prompt.Kind == "environment" {
		return false
	}
	if prompt.Teardown != nil && (answer.Value == nil || answer.Values != nil || answer.Action != "" ||
		*answer.Value != prompt.Teardown.expectedAnswer()) {
		return false
	}
	if prompt.Kind == "actions" {
		if s.resolveEnvironment != nil && s.view.Environment == "" {
			return false
		}
		if answer.Value == nil || *answer.Value == ProjectTeardownAction || !slices.ContainsFunc(prompt.Options, func(option Option) bool {
			return option.Value == *answer.Value
		}) {
			return false
		}
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
		for i := range s.view.SettingsFields {
			field := &s.view.SettingsFields[i]
			if field.ReadOnly {
				continue
			}
			if value, ok := answer.Values[field.Name]; ok {
				if field.Kind == "password" {
					field.Value = ""
					field.Saved = field.Saved || value != ""
				} else {
					field.Value = s.redact(value)
				}
			}
		}
	} else if answer.Value == nil || answer.Values != nil || answer.Action != "" {
		return false
	} else if prompt.Kind == "secret" {
		s.protect(*answer.Value)
	}
	s.submitReply(answer)
	return true
}

// submitReply advances the prompt while holding the session mutex.
func (s *Session) submitReply(answer reply) {
	s.response <- answer
	s.response = nil
	s.view.Prompt = nil
	s.view.Revision++
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
	// The initial settings snapshot can precede vault unlock. Once decrypted
	// credentials are known, redact any matching text already in that snapshot.
	for i := range s.view.SettingsFields {
		field := &s.view.SettingsFields[i]
		field.Value = s.redact(field.Value)
		field.Label = s.redact(field.Label)
		field.Help = s.redact(field.Help)
		for j := range field.Options {
			field.Options[j].Value = s.redact(field.Options[j].Value)
			field.Options[j].Label = s.redact(field.Options[j].Label)
			field.Options[j].Description = s.redact(field.Options[j].Description)
		}
		if field.Action != nil {
			field.Action.Value = s.redact(field.Action.Value)
			field.Action.Label = s.redact(field.Action.Label)
			field.Action.Description = s.redact(field.Action.Description)
		}
	}
	s.view.Revision++
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
	title = s.redact(strings.TrimSpace(title))
	if characters := []rune(title); len(characters) > 200 {
		title = string(characters[:200]) + "…"
	}
	if !slices.Contains(s.view.Visited, phase) {
		s.view.Visited = append(s.view.Visited, phase)
	}
	if title != "" {
		last := len(s.view.Substeps) - 1
		if last < 0 || s.view.Substeps[last].Phase != phase || s.view.Substeps[last].Title != title {
			if last >= 0 && s.view.Substeps[last].Status == "running" {
				s.view.Substeps[last].Status = "complete"
			}
			s.view.Substeps = append(s.view.Substeps, Substep{Phase: phase, Title: title, Status: s.view.Status})
		}
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

func (s *Session) Preview(changes []Change, total int, countsByOwner map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range changes {
		changes[i].Resource = s.redact(changes[i].Resource)
		changes[i].Operation = s.redact(changes[i].Operation)
		changes[i].Feature = s.redact(changes[i].Feature)
		for j, field := range changes[i].Properties {
			changes[i].Properties[j] = PropertyChange{
				Name: s.redact(field.Name), Before: s.redact(field.Before), After: s.redact(field.After),
			}
		}
		for j, field := range changes[i].ReplacementFields {
			changes[i].ReplacementFields[j] = s.redact(field)
		}
	}
	s.view.Changes, s.view.Total, s.view.Reviewed = changes, total, true
	s.view.ChangeCountsByOwner = make(map[string]int, len(countsByOwner))
	for owner, count := range countsByOwner {
		s.view.ChangeCountsByOwner[s.redact(owner)] += count
	}
	s.view.Revision++
}

func (s *Session) Inventory(report reporting.Report) {
	s.InventoryWithSource(report, "pulumi-state")
}

// InventoryWithSource distinguishes a local, unverified snapshot from the
// inventory refreshed during this run. Neither path exposes provider config.
func (s *Session) InventoryWithSource(report reporting.Report, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setInventory(report, source)
	s.view.Revision++
}

func (s *Session) setInventory(report reporting.Report, source string) {
	if old := s.view.Inventory; old != nil &&
		(old.Environment != report.Scope.Environment || old.Stack != report.Scope.Stack ||
			old.Account != report.Scope.Account || old.Region != report.Scope.Region || old.Backend != s.redact(report.Scope.Backend)) {
		s.setDeploymentHistory(nil)
		s.view.Drift = nil
	}
	attachments := reporting.ResolveAttachments(report.Artifacts, s.artifactAttachments)
	items := make([]ArtifactView, 0, len(report.Artifacts))
	for _, a := range report.Artifacts {
		dependencies := make([]string, 0, len(a.Dependencies))
		for _, key := range a.Dependencies {
			dependencies = append(dependencies, s.redact(key))
		}
		items = append(items, ArtifactView{
			Key: s.redact(a.Key), Name: s.redact(a.Name), Type: a.Type,
			Description: s.redact(a.Description),
			IconVariant: s.artifactIconVariants[a.Name],
			Attachment:  s.redact(attachments[a.Key]),
			Component:   a.Component, Parent: s.redact(a.Parent), Owner: s.redact(a.Owner), Feature: s.redact(a.Feature),
			Dependencies: dependencies,
			Action:       a.PlannedAction, Presence: string(a.Observation.Status),
		})
	}
	s.view.Inventory = &InventoryView{
		Source:     source,
		Repository: s.redact(report.Scope.Repository), Environment: report.Scope.Environment,
		Account: report.Scope.Account, Region: report.Scope.Region,
		Project: report.Scope.Project, Backend: s.redact(report.Scope.Backend),
		Stack: report.Scope.Stack, Revision: report.Metadata.Revision, Artifacts: items,
	}
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
	v.CanCheckDrift = s.canCheckDrift()
	v.Activity = slices.Clone(v.Activity)
	v.Visited = slices.Clone(v.Visited)
	v.Substeps = slices.Clone(v.Substeps)
	v.SettingsFields = cloneFields(v.SettingsFields)
	v.ActionOptions = slices.Clone(v.ActionOptions)
	v.Environments = slices.Clone(v.Environments)
	v.OpenEnvironments = slices.Clone(v.OpenEnvironments)
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
	if n := len(s.view.Substeps); n > 0 {
		s.view.Substeps[n-1].Status = s.view.Status
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
	s.resetForActions()
	return true
}

// resetForActions starts a fresh action screen. The caller holds the mutex.
func (s *Session) resetForActions() {
	s.history = nil
	s.view = view{
		Run: s.view.Run + 1, Revision: s.view.Revision + 1, Title: s.view.Title,
		Phase: "settings", PhaseTitle: "Getting ready", Status: "running",
		SettingsFields: s.view.SettingsFields, ActionOptions: s.view.ActionOptions, ActionDefault: s.view.ActionDefault,
		Environment: s.view.Environment, Environments: s.view.Environments,
		OpenEnvironments: s.view.OpenEnvironments, CanRemoveAll: s.view.CanRemoveAll,
	}
	close(s.home)
	s.home = nil
}

// stop handles the intent and run the browser displayed when the button was clicked.
func (s *Session) stop(run uint64, closePage bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vaultLocked() || run != s.view.Run {
		return false
	}
	if closePage {
		atMenu := s.view.Prompt != nil && (s.view.Prompt.Kind == "actions" || s.view.Prompt.Kind == "environment")
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
		if n := len(s.view.Substeps); n > 0 {
			s.view.Substeps[n-1].Status = "stopping"
		}
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
