package wizardweb

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

func TestEnvironmentSelectionOnlyChangesTheIdleTarget(t *testing.T) {
	s := newSession("test", func() {})
	s.ConfigureEnvironments("", []Environment{{Name: "qa", Status: "deployed"}}, func(mode, name string) (EnvironmentTarget, error) {
		if mode != "open" || name != "qa" {
			return EnvironmentTarget{}, errors.New("Invalid target")
		}
		return EnvironmentTarget{
			Inventory: reporting.Report{Scope: reporting.Scope{Environment: "qa", Stack: "test-qa"}},
			Fields:    []Field{{Name: "account", Value: "123456789012"}},
		}, nil
	})
	id := s.view.Revision
	s.view.Prompt = &Prompt{ID: id, Kind: "actions"}
	s.response = make(chan reply, 1)
	handler := s.handler(testHost, "secret")
	post := func(path, token, payload string) int {
		return request(handler, http.MethodPost, path, testHost, "http://"+testHost, token, payload).Code
	}
	selectQA := fmt.Sprintf(`{"id":%d,"mode":"open","name":"qa"}`, id)
	answer := fmt.Sprintf(`{"id":%d,"value":"preview"}`, id)
	if status := post("/api/answer", "secret", answer); status != http.StatusConflict {
		t.Fatalf("preview without an environment returned %d", status)
	}
	if status := post("/api/environment", "", selectQA); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated selection returned %d", status)
	}
	if status := post("/api/environment", "secret", selectQA); status != http.StatusNoContent {
		t.Fatalf("open environment returned %d", status)
	}
	view := s.snapshot()
	if view.Environment != "qa" || view.Inventory.Stack != "test-qa" || view.SettingsFields[0].Name != "account" {
		t.Fatalf("environment view did not switch atomically: %+v", view)
	}
	if status := post("/api/answer", "secret", answer); status != http.StatusConflict {
		t.Fatalf("stale action from before the switch returned %d", status)
	}
	answer = fmt.Sprintf(`{"id":%d,"value":"preview"}`, view.Prompt.ID)
	if status := post("/api/answer", "secret", answer); status != http.StatusNoContent {
		t.Fatalf("current action did not start: %d", status)
	}
	if status := post("/api/environment", "secret", selectQA); status != http.StatusConflict {
		t.Fatalf("running operation changed environment: %d", status)
	}
}
