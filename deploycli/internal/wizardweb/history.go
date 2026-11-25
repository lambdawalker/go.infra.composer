package wizardweb

import (
	"bytes"
	"net/http"
	"time"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

type DeploymentHistoryEntry struct {
	ID          string    `json:"id"`
	FinishedAt  time.Time `json:"finishedAt"`
	Revision    string    `json:"revision,omitempty"`
	Dirty       bool      `json:"dirty,omitempty"`
	Outcome     string    `json:"outcome"`
	ApplyStatus string    `json:"applyStatus,omitempty"`
	Created     int       `json:"created"`
	Updated     int       `json:"updated"`
	Replaced    int       `json:"replaced"`
	Deleted     int       `json:"deleted"`
	Other       int       `json:"other"`
}

type DeploymentHistoryRecord struct {
	DeploymentHistoryEntry
	Report reporting.Report `json:"-"`
}

func (s *Session) SetDeploymentHistory(records []DeploymentHistoryRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setDeploymentHistory(records)
	s.view.Revision++
}

func (s *Session) setDeploymentHistory(records []DeploymentHistoryRecord) {
	s.history = nil
	s.view.DeploymentHistory = nil
	for _, record := range records {
		if record.Report.Scope.Environment == s.view.Environment {
			s.history = append(s.history, record)
			s.view.DeploymentHistory = append(s.view.DeploymentHistory, record.DeploymentHistoryEntry)
		}
	}
}

func (s *Session) historyReport(id string) (reporting.Report, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.vaultLocked() {
		for _, record := range s.history {
			if record.ID == id && record.Report.Scope.Environment == s.view.Environment {
				return record.Report, true
			}
		}
	}
	return reporting.Report{}, false
}

func (s *Session) downloadHistoryReport(w http.ResponseWriter, r *http.Request) {
	report, ok := s.historyReport(r.URL.Query().Get("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	var page bytes.Buffer
	if err := reporting.WriteHTML(&page, report); err != nil {
		http.Error(w, "Could not render the artifact report", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="artifacts.html"`)
	w.Write(page.Bytes())
}
