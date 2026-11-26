package wizardweb

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

func TestDriftCheckOnlyAffectsCurrentSavedTarget(t *testing.T) {
	s := newSession("test", func() {})
	s.ConfigureEnvironments("dev", nil, false, nil)
	s.Inventory(reporting.Report{Scope: reporting.Scope{
		Environment: "dev", Stack: "test-dev", Backend: "s3://state/dev", Account: "123456789012", Region: "us-east-2",
	}})
	id := s.view.Revision
	s.view.Prompt = &Prompt{ID: id, Kind: "actions"}
	answered := make(chan reply, 1)
	s.response = answered
	handler := s.handler(testHost, "secret")
	post := func(token string, id uint64) int {
		intent := fmt.Sprintf(`{"id":%d}`, id)
		return request(handler, http.MethodPost, "/api/drift", testHost, "http://"+testHost, token, intent).Code
	}
	if post("", id) != http.StatusUnauthorized || post("secret", id-1) != http.StatusConflict {
		t.Fatal("unauthenticated or stale request was accepted")
	}
	if !s.snapshot().CanCheckDrift || post("secret", id) != http.StatusNoContent {
		t.Fatal("selected target did not accept drift check")
	}
	if answer := <-answered; answer.Value == nil || *answer.Value != DriftAction {
		t.Fatal("drift check did not use the existing wizard action channel")
	}
	if s.snapshot().CanCheckDrift || post("secret", id) != http.StatusConflict {
		t.Fatal("duplicate request could run alongside the drift check")
	}
	home := s.finish(nil)
	if post("secret", s.view.Revision) != http.StatusNoContent {
		t.Fatal("could not start a drift check directly from the result view")
	}
	select {
	case <-home:
	default:
		t.Fatal("did not advance existing action loop")
	}
	answer, err := s.Ask(t.Context(), Prompt{Kind: "actions"})
	if err != nil || answer != DriftAction {
		t.Fatalf("queued drift action not consumed: %v", err)
	}
}

func TestDriftResultUsesSafeEvidenceAndEnvironment(t *testing.T) {
	s := newSession("test", func() {})
	s.ConfigureEnvironments("dev", nil, false, nil)
	s.Protect("hidden-fixture")
	at := time.Now().UTC()
	report := reporting.Report{Scope: reporting.Scope{Environment: "qa"},
		Metadata: reporting.Metadata{FinishedAt: at, Outcome: "incomplete"},
		Artifacts: []reporting.Artifact{
			{Key: "changed", Name: "hidden-fixture", Observation: reporting.Observation{Status: reporting.Present, Reason: reporting.ExternalChange}},
			{Key: "gone", Observation: reporting.Observation{Status: reporting.Absent}},
			{Key: "unverified", Observation: reporting.Observation{Status: reporting.Unknown}},
			{Key: "unchanged", Observation: reporting.Observation{Status: reporting.Present}},
		},
	}
	s.SetDrift(report)
	if s.snapshot().Drift != nil {
		t.Fatal("drift from another environment reached the browser")
	}
	report.Scope.Environment = "dev"
	s.SetDrift(report)
	got := s.snapshot().Drift
	if got == nil || !got.CheckedAt.Equal(at) || !got.Incomplete || len(got.Changed) != 1 ||
		len(got.Missing) != 1 || len(got.Unverified) != 1 || len(got.Unchanged) != 1 || got.Changed[0].Name == "hidden-fixture" {
		t.Fatalf("unexpected projected drift result: %+v", got)
	}
}
