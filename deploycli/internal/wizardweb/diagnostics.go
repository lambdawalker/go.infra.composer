package wizardweb

import (
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

var (
	terminalEscape  = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b\[[0-?]*[ -/]*[@-~]`)
	credentialLine  = regexp.MustCompile(`(?i)\b(?:[a-z0-9]+[-_])*(authorization|bearer|basic|cookie|access[-_ ]?key(?:[-_ ]?id)?|secret(?:[-_ ]?(?:access[-_ ]?key|key))?|[a-z0-9_-]*token|passphrase|password|api[-_ ]?key|client[-_ ]?secret|private[-_ ]?key|user[-_ ]?code|verification[-_ ]?code|device[-_ ]?code)\b["']?\s*[:=]|\b(?:bearer|basic)\s+[a-z0-9._~+/-]+=*`)
	credentialValue = regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b|\b(?:gh[pousr]_[a-zA-Z0-9]+|github_pat_[a-zA-Z0-9_]+)\b|\beyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]*|[a-zA-Z0-9+/_=-]{40,}`)
	diagnosticURL   = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"']+`)
)

func (s *Session) sanitizeDiagnostic(text string) string {
	text = terminalEscape.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r", "")
	if strings.Contains(text, "PRIVATE KEY-----") {
		return "[Output containing private key material omitted]"
	}
	for _, secret := range s.secrets {
		encoded, _ := json.Marshal(secret)
		text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[redacted]")
	}
	lines := strings.Split(s.redact(text), "\n")
	for i, line := range lines {
		if credentialLine.MatchString(line) {
			lines[i] = "[Credential line redacted]"
			continue
		}
		lines[i] = diagnosticURL.ReplaceAllStringFunc(credentialValue.ReplaceAllString(line, "[redacted]"), func(raw string) string {
			if strings.ContainsAny(raw, "@?#") {
				return "[URL with credentials or parameters redacted]"
			}
			return raw
		})
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (s *Session) CommandFailure(label string, err error, stdout, stderr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := "could not start"
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		status = exit.Error()
	}
	var parts []string
	for _, section := range []struct{ label, text string }{{"Output", stdout}, {"Error output", stderr}} {
		if section.text != "" {
			parts = append(parts, section.label+":\n"+section.text)
		}
	}
	block := label + " (" + status + ")"
	if len(parts) > 0 {
		block += "\n" + strings.Join(parts, "\n")
	}
	// Keep the most recent failed commands within this operation.
	if len(s.commandFailures) == 3 {
		s.commandFailures = s.commandFailures[1:]
	}
	s.commandFailures = append(s.commandFailures, s.sanitizeDiagnostic(block))
}

func (s *Session) failureDiagnostics() string {
	lines := []string{s.sanitizeDiagnostic(s.view.Title)}
	for _, field := range []struct{ label, value string }{
		{"Action", s.view.Action}, {"Stage", s.view.PhaseTitle}, {"Result", s.view.Status},
	} {
		if field.value != "" {
			lines = append(lines, field.label+": "+s.sanitizeDiagnostic(field.value))
		}
	}
	if s.view.Error != "" {
		lines = append(lines, "Error: "+s.view.Error)
	}
	if len(s.commandFailures) > 0 {
		lines = append(lines, "", "Recent failed commands", strings.Join(s.commandFailures, "\n\n"))
	}
	return strings.Join(lines, "\n")
}
