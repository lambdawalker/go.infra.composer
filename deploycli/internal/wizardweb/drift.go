package wizardweb

import (
	"time"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

const DriftAction = "drift"

type DriftArtifact struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type DriftView struct {
	CheckedAt  time.Time       `json:"checkedAt"`
	Incomplete bool            `json:"incomplete"`
	Changed    []DriftArtifact `json:"changed,omitempty"`
	Missing    []DriftArtifact `json:"missing,omitempty"`
	Unverified []DriftArtifact `json:"unverified,omitempty"`
	Unchanged  []DriftArtifact `json:"unchanged,omitempty"`
}

func (s *Session) SetDrift(report reporting.Report) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setDrift(report)
	s.view.Revision++
}

func (s *Session) setDrift(report reporting.Report) {
	s.view.Drift = nil
	if report.Scope.Environment != s.view.Environment || report.Metadata.FinishedAt.IsZero() {
		return
	}
	result := &DriftView{
		CheckedAt: report.Metadata.FinishedAt, Incomplete: report.Metadata.Outcome != "succeeded",
	}
	for _, artifact := range report.Artifacts {
		if artifact.Component {
			continue
		}
		item := DriftArtifact{Key: s.redact(artifact.Key), Name: s.redact(artifact.Name)}
		switch {
		case artifact.Observation.Status == reporting.Present && artifact.Observation.Reason == reporting.ExternalChange:
			result.Changed = append(result.Changed, item)
		case artifact.Observation.Status == reporting.Present:
			result.Unchanged = append(result.Unchanged, item)
		case artifact.Observation.Status == reporting.Absent:
			result.Missing = append(result.Missing, item)
		default:
			result.Unverified = append(result.Unverified, item)
		}
	}
	s.view.Drift = result
}

// canCheckDrift is used while holding the session mutex.
func (s *Session) canCheckDrift() bool {
	inventory, prompt := s.view.Inventory, s.view.Prompt
	if s.vaultLocked() || s.view.Environment == "" || inventory == nil ||
		inventory.Environment != s.view.Environment || inventory.Backend == "" ||
		inventory.Account == "" || inventory.Region == "" || inventory.Stack == "" {
		return false
	}
	return s.home != nil || (s.view.Status == "running" && prompt != nil && prompt.Kind == "actions")
}

func (s *Session) requestDriftCheck(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.canCheckDrift() {
		return false
	}
	if s.home != nil {
		if s.view.Revision != id {
			return false
		}
		s.resetForActions()
		s.pendingDriftRun = s.view.Run
		return true
	}
	if s.view.Prompt.ID != id || s.response == nil {
		return false
	}
	action := DriftAction
	s.submitReply(reply{Value: &action})
	return true
}
