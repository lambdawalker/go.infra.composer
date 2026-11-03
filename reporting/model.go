// Package reporting describes safe artifact inventories and their independent evidence.
package reporting

import (
	"sort"
	"time"
)

const SchemaVersion = 1

type Scope struct {
	Application string `json:"application,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Environment string `json:"environment,omitempty"`
	Account     string `json:"account,omitempty"`
	Region      string `json:"region,omitempty"`
	Project     string `json:"project,omitempty"`
	Backend     string `json:"backend,omitempty"`
	Stack       string `json:"stack,omitempty"`
}
type Status string

const (
	Unknown  Status = "unknown"
	Recorded Status = "recorded"
	Present  Status = "present"
	Absent   Status = "absent"
)

// Reason is a stable code, never provider diagnostic text.
type Reason string

const (
	Unchecked       Reason = "unchecked"
	CheckFailed     Reason = "check_failed"
	InvalidEvidence Reason = "invalid_evidence"
	NotFound        Reason = "not_found"
	IdentityChanged Reason = "identity_changed"
)

type Observation struct {
	Status     Status    `json:"status"`
	Source     string    `json:"source,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
	Reason     Reason    `json:"reason,omitempty"`
}
type Artifact struct {
	Key           string      `json:"key"`
	Name          string      `json:"name,omitempty"`
	Type          string      `json:"type,omitempty"`
	Provider      string      `json:"provider,omitempty"`
	ProviderID    string      `json:"providerId,omitempty"`
	Owner         string      `json:"owner,omitempty"`
	Feature       string      `json:"feature,omitempty"`
	Parent        string      `json:"parent,omitempty"`
	Priority      int         `json:"priority,omitempty"`
	PlannedAction string      `json:"plannedAction,omitempty"`
	Observation   Observation `json:"observation"`
}
type Metadata struct {
	Revision   string    `json:"revision,omitempty"`
	Operation  string    `json:"operation,omitempty"`
	Dirty      bool      `json:"dirty,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}
type Report struct {
	SchemaVersion int        `json:"schemaVersion"`
	Scope         Scope      `json:"scope"`
	Metadata      Metadata   `json:"metadata"`
	CacheLoaded   bool       `json:"cacheLoaded"`
	Warnings      []string   `json:"warnings,omitempty"`
	Artifacts     []Artifact `json:"artifacts"`
}

// Normalize copies and deterministically sorts inventory, preserving evidence timestamps.
// Duplicate logical keys keep the first entry; adapters must supply unique keys.
func Normalize(r Report) Report {
	r.SchemaVersion = SchemaVersion
	r.Artifacts = append([]Artifact(nil), r.Artifacts...)
	r.Warnings = append([]string(nil), r.Warnings...)
	seen := map[string]bool{}
	out := r.Artifacts[:0]
	for _, a := range r.Artifacts {
		if a.Key == "" || seen[a.Key] {
			continue
		}
		seen[a.Key] = true
		switch a.Observation.Status {
		case Present, Absent, Recorded:
			if a.Observation.ObservedAt.IsZero() {
				a.Observation = Observation{Status: Unknown, Reason: InvalidEvidence}
			}
		default:
			a.Observation.Status = Unknown
		}
		switch a.Observation.Reason {
		case "", Unchecked, CheckFailed, InvalidEvidence, NotFound, IdentityChanged:
		default:
			a.Observation.Reason = InvalidEvidence
		}
		if a.Observation.Status == Unknown && a.Observation.Reason == "" {
			a.Observation.Reason = Unchecked
		}
		out = append(out, a)
	}
	r.Artifacts = out
	sort.Slice(r.Artifacts, func(i, j int) bool {
		a, b := r.Artifacts[i], r.Artifacts[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		return a.Key < b.Key
	})
	sort.Strings(r.Warnings)
	return r
}

// Merge reuses evidence only for matching scope, logical key, provider and physical ID.
// Cached-only inventory stays visible. Explicit current unknown evidence supersedes cache.
func Merge(current, cached Report) Report {
	current = Normalize(current)
	cached = Normalize(cached)
	if current.Scope != cached.Scope {
		return current
	}
	current.CacheLoaded = true
	old := map[string]Artifact{}
	for _, a := range cached.Artifacts {
		old[a.Key] = a
	}
	for i, a := range current.Artifacts {
		if b, ok := old[a.Key]; ok {
			if a.ProviderID != "" && a.ProviderID == b.ProviderID && a.Provider == b.Provider && a.Observation.ObservedAt.IsZero() && a.Observation.Reason == Unchecked {
				current.Artifacts[i].Observation = b.Observation
			}
			delete(old, a.Key)
		}
	}
	for _, a := range old {
		a.PlannedAction = ""
		current.Artifacts = append(current.Artifacts, a)
	}
	return Normalize(current)
}

type Node struct {
	Artifact Artifact
	Children []Node
}

// Hierarchy retains orphans and deterministically breaks cycles without losing nodes.
func Hierarchy(artifacts []Artifact) []Node {
	as := Normalize(Report{Artifacts: artifacts}).Artifacts
	by := map[string]Artifact{}
	for _, a := range as {
		by[a.Key] = a
	}
	parents := map[string]string{}
	for _, a := range as {
		if _, ok := by[a.Parent]; ok && a.Parent != a.Key {
			parents[a.Key] = a.Parent
		}
	}
	// Walk in sorted order; remove the first back edge encountered for stable cycle roots.
	for _, a := range as {
		seen := map[string]bool{}
		k := a.Key
		for k != "" {
			seen[k] = true
			p := parents[k]
			if seen[p] {
				delete(parents, k)
				break
			}
			k = p
		}
	}
	children := map[string][]Artifact{}
	for _, a := range as {
		children[parents[a.Key]] = append(children[parents[a.Key]], a)
	}
	var build func(string) []Node
	build = func(k string) []Node {
		var ns []Node
		for _, a := range children[k] {
			ns = append(ns, Node{Artifact: a, Children: build(a.Key)})
		}
		return ns
	}
	return build("")
}

// SelectFeature selects exact canonical feature ownership, including parent closure.
// Parents retain their original Feature/Owner; their inclusion does not imply ownership.
func SelectFeature(as []Artifact, feature string) []Artifact {
	if feature == "" {
		return Normalize(Report{Artifacts: as}).Artifacts
	}
	by := map[string]Artifact{}
	for _, a := range as {
		by[a.Key] = a
	}
	keep := map[string]bool{}
	for _, a := range as {
		if a.Feature != feature {
			continue
		}
		k := a.Key
		for k != "" && !keep[k] {
			b, ok := by[k]
			if !ok {
				break
			}
			keep[k] = true
			k = b.Parent
		}
	}
	var out []Artifact
	for _, a := range as {
		if keep[a.Key] {
			out = append(out, a)
		}
	}
	return Normalize(Report{Artifacts: out}).Artifacts
}
