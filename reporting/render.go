package reporting

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"
)

func WriteJSON(w io.Writer, r Report) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(Normalize(r))
}
func markdown(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "\\\\", "[", "\\[", "]", "\\]", "|", "\\|", "`", "\\`", "*", "\\*", "_", "\\_", "\n", " ", "\r", " ")
	return r.Replace(s)
}
func date(t time.Time) string {
	if t.IsZero() {
		return "not checked"
	}
	return t.UTC().Format(time.RFC3339)
}

type field struct{ Label, Value string }

func contextFields(r Report) []field {
	return []field{{"Application", r.Scope.Application}, {"Repository", r.Scope.Repository}, {"Environment", r.Scope.Environment}, {"Account", r.Scope.Account}, {"Region", r.Scope.Region}, {"Backend", r.Scope.Backend}, {"Project", r.Scope.Project}, {"Stack", r.Scope.Stack}, {"Revision", r.Metadata.Revision}, {"Operation", r.Metadata.Operation}, {"Started", date(r.Metadata.StartedAt)}, {"Finished", date(r.Metadata.FinishedAt)}, {"Duration", duration(r.Metadata)}}
}
func duration(m Metadata) string {
	if m.StartedAt.IsZero() || m.FinishedAt.IsZero() {
		return "incomplete"
	}
	return m.FinishedAt.Sub(m.StartedAt).String()
}
func summary(r Report) string {
	counts := map[Status]int{}
	changes := 0
	for _, a := range r.Artifacts {
		counts[a.Observation.Status]++
		if a.PlannedAction != "" && a.PlannedAction != "same" {
			changes++
		}
	}
	main := len(Hierarchy(r.Artifacts))
	return fmt.Sprintf("%d main artifacts · %d children · %d present · %d absent · %d recorded · %d unknown · %d planned changes", main, len(r.Artifacts)-main, counts[Present], counts[Absent], counts[Recorded], counts[Unknown], changes)
}
func issues(r Report) []Artifact {
	var as []Artifact
	for _, a := range r.Artifacts {
		if a.Observation.Status == Absent || a.Observation.Reason == CheckFailed || strings.Contains(a.PlannedAction, "delete") || strings.Contains(a.PlannedAction, "replace") {
			as = append(as, a)
		}
	}
	return as
}
func artifactFields(a Artifact) []field {
	return []field{{"Logical identity", a.Key}, {"Type", a.Type}, {"Provider ID", a.ProviderID}, {"Owner", a.Owner}, {"Feature", a.Feature}, {"Planned action", a.PlannedAction}, {"Presence", string(a.Observation.Status)}, {"Source", a.Observation.Source}, {"Observed", date(a.Observation.ObservedAt)}, {"Reason", string(a.Observation.Reason)}}
}
func display(a Artifact) string {
	if a.Name != "" {
		return a.Name
	}
	return a.Key
}
func WriteMarkdown(w io.Writer, r Report) error {
	r = Normalize(r)
	var b strings.Builder
	fmt.Fprintf(&b, "# Artifact report\n\nOutcome: **%s**\n\n%s\n\nCached evidence loaded: %t\n\n", markdown(r.Metadata.Outcome), summary(r), r.CacheLoaded)
	for _, f := range contextFields(r) {
		fmt.Fprintf(&b, "- %s: %s\n", f.Label, markdown(f.Value))
	}
	if is := issues(r); len(is) > 0 {
		b.WriteString("\n## Attention\n\n")
		for _, a := range is {
			fmt.Fprintf(&b, "- %s: %s; %s (%s)\n", markdown(display(a)), markdown(a.PlannedAction), a.Observation.Status, a.Observation.Reason)
		}
	}
	for _, warning := range r.Warnings {
		fmt.Fprintf(&b, "\nWarning: %s\n", markdown(warning))
	}
	var walk func([]Node, int)
	walk = func(ns []Node, d int) {
		for _, n := range ns {
			a := n.Artifact
			fmt.Fprintf(&b, "\n%s %s\n\n", strings.Repeat("#", min(d+2, 6)), markdown(display(a)))
			for _, f := range artifactFields(a) {
				fmt.Fprintf(&b, "- %s: %s\n", f.Label, markdown(f.Value))
			}
			walk(n.Children, d+1)
		}
	}
	walk(Hierarchy(r.Artifacts), 0)
	_, e := io.WriteString(w, b.String())
	return e
}

var page = template.Must(template.New("report").Funcs(template.FuncMap{"fields": artifactFields, "display": display}).Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Artifact report</title><style>body{font:16px system-ui;max-width:1050px;margin:3rem auto;padding:0 1rem;background:#f6f8fb;color:#172235}article,details,.attention{background:white;border:1px solid #ccd5df;border-radius:8px;padding:1rem;margin:.6rem 0}.attention{border-left:5px solid #b34b22}details details{margin-left:1rem}dl{display:grid;grid-template-columns:minmax(110px,1fr) 4fr;gap:.35rem}dt{font-weight:bold}dd{margin:0;overflow-wrap:anywhere}summary{cursor:pointer}small{color:#46556a}</style><h1>Artifact report</h1><p><strong>Outcome: {{.Metadata.Outcome}}</strong></p><p>{{.Summary}}</p><p>Cached evidence loaded: {{.CacheLoaded}}</p><dl>{{range .Context}}<dt>{{.Label}}</dt><dd>{{.Value}}</dd>{{end}}</dl>{{if .Issues}}<section class="attention"><h2>Attention</h2>{{range .Issues}}<p>{{display .}}: {{.PlannedAction}}; {{.Observation.Status}} ({{.Observation.Reason}})</p>{{end}}</section>{{end}}{{range .Warnings}}<p>Warning: {{.}}</p>{{end}}{{range .Nodes}}<article><h2>{{display .Artifact}} — {{.Artifact.Observation.Status}}</h2>{{template "fields" .Artifact}}{{template "children" .Children}}</article>{{end}}{{define "fields"}}<dl>{{range fields .}}<dt>{{.Label}}</dt><dd>{{.Value}}</dd>{{end}}</dl>{{end}}{{define "children"}}{{range .}}<details><summary>{{display .Artifact}} — {{.Artifact.Observation.Status}}</summary>{{template "fields" .Artifact}}{{template "children" .Children}}</details>{{end}}{{end}}</html>`))

func WriteHTML(w io.Writer, r Report) error {
	r = Normalize(r)
	return page.Execute(w, struct {
		Report
		Nodes   []Node
		Context []field
		Summary string
		Issues  []Artifact
	}{r, Hierarchy(r.Artifacts), contextFields(r), summary(r), issues(r)})
}
