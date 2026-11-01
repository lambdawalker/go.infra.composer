package composer

import (
	"fmt"
	"strings"
)

type Diagnostic struct {
	Code       string   `json:"code"`
	Message    string   `json:"message"`
	Source     string   `json:"source"`
	Suggestion string   `json:"suggestion"`
	Path       []string `json:"path,omitempty"`
	Details    []string `json:"details,omitempty"`
}
type ValidationError struct {
	Diagnostics       []Diagnostic `json:"diagnostics"`
	DeploymentStarted bool         `json:"deployment_started"`
}

func (e *ValidationError) Error() string {
	var s strings.Builder
	fmt.Fprintln(&s, "DEPLOYMENT BLOCKED")
	for _, d := range e.Diagnostics {
		fmt.Fprintf(&s, "[%s] %s\n  declared at: %s\n", d.Code, d.Message, d.Source)
		for _, x := range d.Details {
			fmt.Fprintf(&s, "  %s\n", x)
		}
		if len(d.Path) > 0 {
			fmt.Fprintf(&s, "  cycle: %s\n", strings.Join(d.Path, " -> "))
		}
		fmt.Fprintf(&s, "  Suggestion: %s\n", d.Suggestion)
	}
	fmt.Fprint(&s, "No deployment was started.")
	return s.String()
}
func diagnostic(code, msg, src, suggestion string) Diagnostic {
	return Diagnostic{Code: code, Message: msg, Source: source(src, "composition"), Suggestion: suggestion}
}
func invalid(code, msg, src, suggestion string) error {
	return &ValidationError{Diagnostics: []Diagnostic{diagnostic(code, msg, src, suggestion)}}
}
