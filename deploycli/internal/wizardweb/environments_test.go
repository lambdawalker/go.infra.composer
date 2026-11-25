package wizardweb

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/lambdawalker/go.infra.composer/reporting"
)

func TestEnvironmentSelectionOnlyChangesTheIdleTarget(t *testing.T) {
	s := newSession("test", func() {})
	s.ConfigureEnvironments("", []Environment{{Name: "qa", Status: "deployed"}}, false, func(mode, name string) (EnvironmentTarget, error) {
		if mode != "open" || name != "qa" {
			return EnvironmentTarget{}, errors.New("Invalid target")
		}
		return EnvironmentTarget{
			Inventory: reporting.Report{Scope: reporting.Scope{Environment: "qa", Stack: "test-qa"}},
			Fields:    []Field{{Name: "account", Value: "123456789012"}},
		}, nil
	})
	id := s.view.Revision
	s.view.Prompt = &Prompt{ID: id, Kind: "actions", Options: []Option{{Value: "preview"}}}
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

func TestOpenEnvironmentTabsSurviveSwitchAndActionResult(t *testing.T) {
	s := newSession("test", func() {})
	s.ConfigureEnvironments("dev", []Environment{{Name: "qa", Status: "deployed"}}, false, func(mode, name string) (EnvironmentTarget, error) {
		return EnvironmentTarget{Inventory: reporting.Report{Scope: reporting.Scope{Environment: name}}}, nil
	})
	s.view.Prompt = &Prompt{ID: s.view.Revision, Kind: "actions"}
	if err := s.selectEnvironment(s.view.Prompt.ID, "switch", "qa"); err == nil {
		t.Fatal("switched to an unopened environment")
	}
	if err := s.selectEnvironment(s.view.Prompt.ID, "open", "qa"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dev", "qa"} {
		if err := s.selectEnvironment(s.view.Prompt.ID, "switch", name); err != nil {
			t.Fatal(err)
		}
		if state := s.snapshot(); state.Environment != name || state.Inventory.Environment != name {
			t.Fatalf("switch kept the wrong target: %+v", state)
		}
	}
	s.Note("Previous environment output")
	home := s.finish(nil)
	result := s.snapshot()
	if err := s.selectEnvironment(result.Revision, "switch", "dev"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-home:
	default:
		t.Fatal("switching from a result did not return to the action menu")
	}
	state := s.snapshot()
	if !slices.Equal(state.OpenEnvironments, []string{"dev", "qa"}) || state.Environment != "dev" ||
		state.Run != result.Run+1 || state.Inventory.Environment != "dev" || len(state.Activity) != 0 {
		t.Fatalf("switch did not preserve tabs and clear the old result: %+v", state)
	}
}

func TestProjectTeardownRequiresMenuCommand(t *testing.T) {
	s := newSession("test", func() {})
	s.ConfigureEnvironments("qa", nil, true, nil)
	id := s.view.Revision
	s.view.Prompt = &Prompt{ID: id, Kind: "actions", Options: []Option{{Value: "preview"}}}
	answered := make(chan reply, 1)
	s.response = answered
	handler := s.handler(testHost, "secret")
	post := func(path, token, payload string) int {
		return request(handler, http.MethodPost, path, testHost, "http://"+testHost, token, payload).Code
	}
	intent := fmt.Sprintf(`{"id":%d}`, id)
	action := fmt.Sprintf(`{"id":%d,"value":"destroy-all"}`, id)
	if status := post("/api/answer", "secret", action); status != http.StatusConflict {
		t.Fatalf("project teardown was accepted as an environment action: %d", status)
	}
	if status := post("/api/project-teardown", "", intent); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated teardown request returned %d", status)
	}
	if status := post("/api/project-teardown", "secret", intent); status != http.StatusNoContent {
		t.Fatalf("project teardown menu request returned %d", status)
	}
	select {
	case answer := <-answered:
		if answer.Value == nil || *answer.Value != ProjectTeardownAction {
			t.Fatal("menu did not select the project-wide teardown flow")
		}
	default:
		t.Fatal("project menu did not advance the action prompt")
	}
	if status := post("/api/project-teardown", "secret", intent); status != http.StatusConflict {
		t.Fatalf("repeated teardown request returned %d", status)
	}
}
