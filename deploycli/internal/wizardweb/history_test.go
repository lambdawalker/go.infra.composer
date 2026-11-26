package wizardweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

func TestDeploymentHistoryReportAuthorizationAndEnvironment(t *testing.T) {
	s := newSession("test", func() {})
	s.openEnvironment("dev")
	s.setDeploymentHistory([]DeploymentHistoryRecord{{
		DeploymentHistoryEntry: DeploymentHistoryEntry{ID: "dev-history"},
		Report: reporting.Report{
			Scope:     reporting.Scope{Environment: "dev"},
			Artifacts: []reporting.Artifact{{Key: "fixture", Name: "<script>alert(1)</script>"}},
		},
	}})
	request := func(id string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		const host = "127.0.0.1:9000"
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/deployment-history/report?id="+id, nil)
		if authenticated {
			req.Header.Set("Authorization", "Bearer test")
		}
		res := httptest.NewRecorder()
		s.handler(host, "test").ServeHTTP(res, req)
		return res
	}
	if got := request("dev-history", false); got.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized status = %d", got.Code)
	}
	got := request("dev-history", true)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "&lt;script&gt;") ||
		strings.Contains(got.Body.String(), "<script>alert(1)</script>") ||
		!strings.HasPrefix(got.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("unexpected report response: %d %v", got.Code, got.Header())
	}
	if got := request("../../report", true); got.Code != http.StatusNotFound {
		t.Errorf("arbitrary path status = %d", got.Code)
	}
	s.openEnvironment("qa")
	if got := request("dev-history", true); got.Code != http.StatusNotFound {
		t.Errorf("other environment status = %d", got.Code)
	}
}
