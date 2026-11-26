package wizardweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lambdawalker/go.infra.composer/deploycli/awsicons"
	"github.com/lambdawalker/go.infra.composer/reporting"
)

func TestInventoryExposesSafeFieldsAndClearsBetweenRuns(t *testing.T) {
	s := newSession("test wizard", func() {})
	s.Protect("secret-value")
	s.InventoryWithSource(reporting.Report{
		Scope: reporting.Scope{Repository: "github.com/example/app", Backend: "s3://secret-value", Stack: "test-dev"},
		Artifacts: []reporting.Artifact{{
			Key: "urn:pulumi:secret-value", Name: "example", Type: "aws:lambda/function:Function",
			Description:   "Runs secret-value verification requests.",
			PlannedAction: "update", Observation: reporting.Observation{Status: reporting.Recorded},
		}},
	}, "saved-report")
	v := s.snapshot()
	if v.Inventory == nil || len(v.Inventory.Artifacts) != 1 || v.Inventory.Backend != "s3://[redacted]" ||
		v.Inventory.Artifacts[0].Key != "urn:pulumi:[redacted]" || v.Inventory.Artifacts[0].Description != "Runs [redacted] verification requests." ||
		v.Inventory.Artifacts[0].Presence != "recorded" || v.Inventory.Source != "saved-report" {
		t.Fatalf("unsafe or incomplete inventory: %+v", v.Inventory)
	}
	// Returning to the action menu must not display the previous stack inventory.
	s.finish(nil)
	if !s.returnToActions(s.snapshot().Revision) {
		t.Fatal("could not return to menu")
	}
	if s.snapshot().Inventory != nil {
		t.Fatal("inventory persisted after menu reset")
	}
}

func TestSettingsAndActionsSnapshotsStaySafeAcrossRuns(t *testing.T) {
	s := newSession("test wizard", func() {})
	s.Protect("private-token")
	fields := []Field{
		{Name: "domain", Label: "Domain", Value: "private-token.example", Kind: "select",
			Options: []Option{{Value: "private-token", Label: "private-token"}},
			Action:  &Option{Value: "private-token", Label: "Refresh private-token"}},
		{Name: "credential", Label: "API token", Kind: "password", Value: "private-token"},
	}
	s.SetSettingsFields(fields)
	s.SetActionOptions([]Option{{Value: "preview", Label: "Preview changes"}}, "preview")
	s.SetPasswordSaved(map[string]bool{"credential": true})
	fields[0].Options[0].Value = "mutated outside session"
	first := s.snapshot()
	data, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-token") || first.SettingsFields[0].Value != "[redacted].example" ||
		first.SettingsFields[1].Value != "" || !first.SettingsFields[1].Saved ||
		first.SettingsFields[0].Options[0].Value != "[redacted]" {
		t.Fatalf("unsafe settings snapshot: %s", data)
	}
	first.SettingsFields[0].Options[0].Value = "mutated snapshot"
	first.SettingsFields[0].Action.Label = "mutated snapshot"
	first.ActionOptions[0].Label = "mutated snapshot"
	if got := s.snapshot(); got.SettingsFields[0].Options[0].Value != "[redacted]" ||
		got.SettingsFields[0].Action.Label != "Refresh [redacted]" || got.ActionOptions[0].Label != "Preview changes" {
		t.Fatalf("snapshot fields alias state: %+v", got)
	}
	response := request(s.handler(testHost, "session-key"), http.MethodGet, "/api/state", testHost, "", "session-key", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-token") ||
		!strings.Contains(response.Body.String(), `"saved":true`) {
		t.Fatalf("API exposed a stored token or omitted the saved marker: %d %s", response.Code, response.Body.String())
	}

	s.mu.Lock()
	s.view.Prompt = &Prompt{ID: 99, Kind: "form", Fields: []Field{{Name: "domain"}, {Name: "credential", Kind: "password"}}}
	s.response = make(chan reply, 1)
	s.mu.Unlock()
	if !s.answer(99, reply{Values: map[string]string{"domain": "changed.example", "credential": "new-token"}}) {
		t.Fatal("form response rejected")
	}
	got := s.snapshot()
	if got.SettingsFields[0].Value != "changed.example" || got.SettingsFields[1].Value != "" || !got.SettingsFields[1].Saved {
		t.Fatalf("submitted values not reflected safely: %+v", got.SettingsFields)
	}
	s.finish(nil)
	if !s.returnToActions(s.snapshot().Revision) {
		t.Fatal("could not return to actions")
	}
	got = s.snapshot()
	if len(got.SettingsFields) != 2 || got.SettingsFields[0].Value != "changed.example" ||
		len(got.ActionOptions) != 1 || got.ActionDefault != "preview" || got.Prompt != nil {
		t.Fatalf("settings and actions did not survive menu reset: %+v", got)
	}
	data, err = json.Marshal(got)
	if err != nil || strings.Contains(string(data), "new-token") {
		t.Fatalf("secret persisted across runs: %s, %v", data, err)
	}
}

func TestLateCredentialUnlockRedactsInitialSettingsSnapshot(t *testing.T) {
	s := newSession("test wizard", func() {})
	s.SetSettingsFields([]Field{{Name: "feature.note", Kind: "text", Value: "late-secret.example"},
		{Name: "credential", Kind: "password", Value: "late-secret"}})
	s.Protect("late-secret")
	response := request(s.handler(testHost, "session-key"), http.MethodGet, "/api/state", testHost, "", "session-key", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "late-secret") ||
		!strings.Contains(response.Body.String(), "[redacted].example") {
		t.Fatalf("late credential remained in startup state: %d %s", response.Code, response.Body.String())
	}
}

func TestSubstepHistorySurvivesSnapshotsAndResetsBetweenRuns(t *testing.T) {
	s := newSession("test wizard", func() {})
	s.Protect("private-token")
	s.Action("deploy")
	s.Phase("connect", "Verify private-token access")
	s.Phase("connect", "Verify private-token access") // Duplicate update, not a new step.
	s.Phase("prepare", "Prepare the Pulumi stack")
	s.Phase("prepare", "Build Lambda")
	s.Phase("prepare", "Prepare the Pulumi stack") // Revisiting a stage is visible.
	v := s.snapshot()
	if len(v.Substeps) != 4 || v.Substeps[0].Title != "Verify [redacted] access" ||
		v.Substeps[0].Status != "complete" || v.Substeps[2].Status != "complete" ||
		v.Substeps[3].Status != "running" {
		t.Fatalf("incomplete substep history: %+v", v.Substeps)
	}
	v.Substeps[0].Title = "changed outside session"
	if got := s.snapshot().Substeps[0].Title; got != "Verify [redacted] access" {
		t.Fatalf("snapshot exposed session memory: %q", got)
	}
	s.finish(errors.New("preview failed"))
	v = s.snapshot()
	if v.Substeps[3].Status != "failed" {
		t.Fatalf("failure not reflected in last substep: %+v", v.Substeps)
	}
	if !s.returnToActions(v.Revision) || len(s.snapshot().Substeps) != 0 {
		t.Fatal("substeps were carried into the next run")
	}
}

func TestSubstepTracksStoppingAndCancellation(t *testing.T) {
	s := newSession("test wizard", func() {})
	s.Phase("apply", "Apply the saved plan")
	if !s.stop(s.snapshot().Run, false) || s.snapshot().Substeps[0].Status != "stopping" {
		t.Fatal("current substep did not enter stopping state")
	}
	s.finish(context.Canceled)
	if got := s.snapshot().Substeps[0].Status; got != "cancelled" {
		t.Fatalf("current substep ended %q, want cancelled", got)
	}
}

const testHost = "127.0.0.1:17774"

func request(handler http.Handler, method, path, host, origin, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestLoopbackWizardProtectsStateAndReplies(t *testing.T) {
	session := newSession("test wizard", func() {})
	session.repository = "lambdawalker/go.apexfission.aws.email-verification"
	session.view.Prompt = &Prompt{ID: 7, Kind: "secret", Label: "Vault passphrase"}
	session.response = make(chan reply, 1)
	replies := session.response
	handler := session.handler(testHost, "known-token")

	for _, tc := range []struct {
		host, origin, token string
		status              int
	}{
		{testHost, "", "known-token", http.StatusOK},
		{testHost, "", "", http.StatusUnauthorized},
		{"other.example", "", "known-token", http.StatusForbidden},
		{testHost, "https://other.example", "known-token", http.StatusForbidden},
	} {
		rec := request(handler, http.MethodGet, "/api/state", tc.host, tc.origin, tc.token, "")
		if rec.Code != tc.status || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("state status %d, want %d", rec.Code, tc.status)
		}
	}
	rec := request(handler, http.MethodGet, "/", testHost, "", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "wizard.js") ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("local UI or content security policy missing")
	}
	if !strings.Contains(rec.Body.String(), "topology-tab") {
		t.Fatal("topology tab missing from local UI")
	}
	for _, markup := range []string{
		`<title>Deploy: go.apexfission.aws.email-verification</title>`,
		`href="https://github.com/lambdawalker/go.apexfission.aws.email-verification"`,
		`>/lambdawalker/go.apexfission.aws.email-verification</a>`,
		`<link rel="icon" type="image/svg+xml" href="/favicon.svg">`,
	} {
		if !strings.Contains(rec.Body.String(), markup) {
			t.Errorf("wizard page is missing %s", markup)
		}
	}
	if strings.Contains(rec.Body.String(), "Local session") {
		t.Error("old session badge is still in the header")
	}
	favicon := request(handler, http.MethodGet, "/favicon.svg", testHost, "", "", "")
	if favicon.Code != http.StatusOK || !strings.Contains(favicon.Header().Get("Content-Type"), "image/svg+xml") ||
		!strings.HasPrefix(favicon.Body.String(), "<svg") {
		t.Fatalf("deployment favicon was not served locally: %d %s", favicon.Code, favicon.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), `src="/aws-icons.js"`) {
		t.Fatal("AWS icon mapper missing from local UI")
	}
	for _, asset := range []string{"/topology.js", "/topology.css", "/vendor/topology-three.js"} {
		assetResponse := request(handler, http.MethodGet, asset, testHost, "", "", "")
		if assetResponse.Code != http.StatusOK || assetResponse.Body.Len() == 0 ||
			strings.Contains(assetResponse.Header().Get("Content-Security-Policy"), "unsafe-eval") {
			t.Fatalf("topology asset %s or restrictive CSP missing: %d", asset, assetResponse.Code)
		}
	}
	iconMap := request(handler, http.MethodGet, "/aws-icons.js", testHost, "", "", "")
	if iconMap.Code != http.StatusOK || !strings.Contains(iconMap.Body.String(), "awsIconFor") {
		t.Fatalf("AWS icon mapper was not served locally: %d", iconMap.Code)
	}
	icons, err := fs.Glob(awsicons.Files(), "icons/*/*.svg")
	if err != nil || len(icons) == 0 {
		t.Fatalf("listing AWS service icons: %v (%d icons)", err, len(icons))
	}
	for _, name := range icons {
		asset := "/" + name
		response := request(handler, http.MethodGet, asset, testHost, "", "", "")
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "image/svg+xml") ||
			!strings.Contains(response.Header().Get("Content-Security-Policy"), "img-src 'self'") ||
			!strings.Contains(response.Body.String(), "<svg") {
			t.Fatalf("AWS service icon %s was not served locally: %d %s", asset, response.Code, response.Header().Get("Content-Type"))
		}
	}
	if response := request(handler, http.MethodGet, "/icons/aws/unknown.svg", testHost, "", "", ""); response.Code != http.StatusNotFound {
		t.Fatalf("unlisted icon was served: %d", response.Code)
	}
	for _, name := range icons {
		asset := "/models/" + strings.TrimSuffix(strings.TrimPrefix(name, "icons/"), ".svg") + ".glb"
		response := request(handler, http.MethodGet, asset, testHost, "", "", "")
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "model/gltf-binary" ||
			!strings.HasPrefix(response.Body.String(), "glTF") {
			t.Fatalf("AWS service model %s was not served as GLB: %d", asset, response.Code)
		}
	}
	for _, asset := range []string{"/models/aws/", "/icons/aws/", "/icons/aws/../aws/s3.svg", "/models/aws/unknown.glb", "/models/aws/README.md", "/vendor/three-LICENSE.txt"} {
		if response := request(handler, http.MethodGet, asset, testHost, "", "", ""); response.Code != http.StatusNotFound {
			t.Fatalf("unlisted model asset was served: %s: %d", asset, response.Code)
		}
	}
	post := func(body string) int {
		return request(handler, http.MethodPost, "/api/answer", testHost, "http://"+testHost, "known-token", body).Code
	}
	if got := post(`{"id":6,"value":"old"}`); got != http.StatusConflict {
		t.Fatalf("stale reply returned %d", got)
	}
	if got := post(`{"id":7,"value":"local-passphrase"}`); got != http.StatusNoContent {
		t.Fatalf("valid reply returned %d", got)
	}
	if got := <-replies; got.Value == nil || *got.Value != "local-passphrase" {
		t.Fatalf("reply was not delivered")
	}
	if got := post(`{"id":7,"value":"duplicate"}`); got != http.StatusConflict {
		t.Fatalf("duplicate reply returned %d", got)
	}

	session.Note("diagnostic: local-passphrase")
	writer := session.SignInWriter()
	_, _ = writer.Write([]byte("local-pass"))
	_, _ = writer.Write([]byte("phrase\n"))
	rec = request(handler, http.MethodGet, "/api/state", testHost, "", "known-token", "")
	var state view
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "local-passphrase") || !strings.Contains(state.SignIn, "[redacted]") {
		t.Fatal("the browser state contained a protected value")
	}
	session.ClearSignIn()
	if session.snapshot().SignIn != "" {
		t.Fatal("sign-in output stayed visible after authentication")
	}
}

func TestStopDeploymentCanReturnAfterCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		page, closePage := context.WithCancel(t.Context())
		defer closePage()
		session := newSession("deploy", closePage)
		handler := session.handler(testHost, "known-token")
		post := func(path, body string) int {
			return request(handler, http.MethodPost, path, testHost, "http://"+testHost, "known-token", body).Code
		}

		cleaned := make(chan struct{})
		finishCleanup := make(chan struct{}, 1)
		defer close(finishCleanup)
		finished := make(chan error, 1)
		go func() {
			finished <- runSession(page, session, func(ctx context.Context, s *Session, showActions bool) error {
				if showActions {
					<-cleaned
					s.Action("status")
					return ctx.Err()
				}
				s.Action("deploy")
				s.Preview([]Change{{Resource: "fixture", Operation: "create"}}, 1, nil)
				<-ctx.Done()
				<-finishCleanup
				close(cleaned)
				return ctx.Err()
			})
		}()
		synctest.Wait()

		if got := post("/api/stop", `{"run":1,"close":false}`); got != http.StatusNoContent {
			t.Fatalf("stop returned %d", got)
		}
		synctest.Wait()
		stopping := session.snapshot()
		if page.Err() != nil || stopping.Status != "stopping" {
			t.Fatalf("operation still cleaning up: %+v", stopping)
		}
		if got := post("/api/actions", fmt.Sprintf(`{"revision":%d}`, stopping.Revision)); got != http.StatusConflict {
			t.Fatalf("return before cleanup returned %d", got)
		}
		finishCleanup <- struct{}{}
		synctest.Wait()
		stopped := session.snapshot()
		if stopped.Status != "cancelled" || !stopped.Reviewed {
			t.Fatalf("stopped deployment result: %+v", stopped)
		}
		if got := post("/api/stop", `{"run":1,"close":false}`); got != http.StatusConflict || page.Err() != nil {
			t.Fatalf("repeated stop affected the session: %d", got)
		}

		if got := post("/api/actions", fmt.Sprintf(`{"revision":%d}`, stopped.Revision)); got != http.StatusNoContent {
			t.Fatalf("return after cleanup returned %d", got)
		}
		synctest.Wait()
		state := session.snapshot()
		if state.Run != 2 || state.Action != "status" || state.Reviewed || state.Status != "complete" {
			t.Fatalf("unexpected next action state: %+v", state)
		}
		if got := post("/api/stop", `{"run":1,"close":true}`); got != http.StatusConflict || page.Err() != nil {
			t.Fatalf("stale close affected the session: %d", got)
		}
		if got := post("/api/stop", `{"run":2,"close":true}`); got != http.StatusNoContent || page.Err() == nil {
			t.Fatalf("close did not end the session: %d", got)
		}
		if err := <-finished; err != nil {
			t.Fatalf("next action ran with a cancelled context: %v", err)
		}
	})
}

func TestFormAcceptsOnlyEditableFieldsAsOneReply(t *testing.T) {
	session := newSession("test wizard", func() {})
	session.view.Prompt = &Prompt{ID: 9, Kind: "form", Fields: []Field{
		{Name: "sender", Action: &Option{Value: "refresh-profiles"}}, {Name: "recipient"}, {Name: "account", ReadOnly: true},
	}}
	session.response = make(chan reply, 1)
	replies := session.response
	handler := session.handler(testHost, "known-token")
	post := func(body string) int {
		return request(handler, http.MethodPost, "/api/answer", testHost, "http://"+testHost, "known-token", body).Code
	}
	for _, body := range []string{
		`{"id":9,"value":"continue"}`,
		`{"id":9,"values":{"sender":"one"}}`,
		`{"id":9,"values":{"sender":"one","recipient":"two","account":"override"}}`,
		`{"id":9,"values":{"sender":"one","recipient":"two"},"action":"unknown"}`,
	} {
		if got := post(body); got != http.StatusConflict {
			t.Fatalf("malformed form returned %d for %s", got, body)
		}
	}
	if got := post(`{"id":9,"values":{"sender":"one","recipient":"two"},"action":"refresh-profiles"}`); got != http.StatusNoContent {
		t.Fatalf("valid form returned %d", got)
	}
	if got := <-replies; got.Values["sender"] != "one" || got.Values["recipient"] != "two" || got.Action != "refresh-profiles" {
		t.Fatalf("form reply was not sent as a batch: %+v", got)
	}
}

func TestFormPreviewDoesNotAnswerPrompt(t *testing.T) {
	session := newSession("test", func() {})
	session.view.Prompt = &Prompt{
		ID: 9, Kind: "form", PreviewFields: []string{"domain"},
		Fields: []Field{{Name: "domain"}, {Name: "profile"}},
		Preview: func(values map[string]string) FormPreview {
			return FormPreview{Values: map[string]string{"sender": "verify@" + values["domain"]}, Errors: map[string]string{"domain": "example issue"}}
		},
	}
	session.response = make(chan reply, 1)
	handler := session.handler(testHost, "known-token")
	post := func(token, body string) *httptest.ResponseRecorder {
		return request(handler, http.MethodPost, "/api/form-preview", testHost, "http://"+testHost, token, body)
	}
	valid := `{"id":9,"values":{"domain":"draft.example"}}`
	if got := post("", valid).Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated preview: %d", got)
	}
	if got := post("known-token", `{"id":8,"values":{"domain":"draft.example"}}`).Code; got != http.StatusConflict {
		t.Fatalf("preview for stale prompt: %d", got)
	}
	if got := post("known-token", `{"id":9,"values":{"other":"draft.example"}}`).Code; got != http.StatusConflict {
		t.Fatalf("preview with an unrecognized field: %d", got)
	}
	rec := post("known-token", valid)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "verify@draft.example") || !strings.Contains(rec.Body.String(), "example issue") {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	if session.snapshot().Prompt.ID != 9 || len(session.response) > 0 {
		t.Fatal("preview advanced the prompt")
	}
}

func TestReturnToActionsAfterEachResult(t *testing.T) {
	session := newSession("test wizard", func() {})
	handler := session.handler(testHost, "known-token")
	post := func(revision uint64, token string) int {
		body := fmt.Sprintf(`{"revision":%d}`, revision)
		return request(handler, http.MethodPost, "/api/actions", testHost, "http://"+testHost, token, body).Code
	}
	if got := post(session.snapshot().Revision, "known-token"); got != http.StatusConflict {
		t.Fatalf("returned while first action was running: %d", got)
	}

	for _, tc := range []struct {
		action, status string
		err            error
	}{
		{action: "setup", status: "complete"},
		{action: "preview", status: "complete"},
		{action: "report", status: "failed", err: errors.New("report failed")},
		{action: "github-setup", status: "cancelled", err: context.Canceled},
	} {
		session.Action(tc.action)
		session.Phase("prepare", "Prepare deployment")
		session.Note("previous activity")
		home := session.finish(tc.err)
		result := session.snapshot()
		if result.Status != tc.status || len(result.Activity) != 1 {
			t.Fatalf("%s result unavailable: %+v", tc.action, result)
		}
		if got := post(result.Revision, ""); got != http.StatusUnauthorized {
			t.Fatalf("unauthenticated return: %d", got)
		}
		if got := post(result.Revision-1, "known-token"); got != http.StatusConflict {
			t.Fatalf("stale return: %d", got)
		}
		if got := post(result.Revision, "known-token"); got != http.StatusNoContent {
			t.Fatalf("return after %s: %d", tc.action, got)
		}
		select {
		case <-home:
		default:
			t.Fatal("return did not wake the wizard")
		}
		state := session.snapshot()
		if state.Run != result.Run+1 || state.Status != "running" || state.Phase != "settings" ||
			state.Action != "" || state.Error != "" || len(state.Activity) != 0 || len(state.Visited) != 0 || len(state.Substeps) != 0 {
			t.Fatalf("previous action state carried over: %+v", state)
		}
		if got := post(result.Revision, "known-token"); got != http.StatusConflict {
			t.Fatalf("duplicate return: %d", got)
		}
	}
}

func TestActionMenuCanWaitAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		page, closePage := context.WithCancel(t.Context())
		defer closePage()
		session := newSession("test wizard", closePage)
		finished := make(chan error, 1)
		go func() {
			finished <- runSession(page, session, func(ctx context.Context, s *Session, _ bool) error {
				_, err := s.Ask(ctx, Prompt{Kind: "actions", Label: "Choose an action"})
				return err
			})
		}()
		synctest.Wait()
		time.Sleep(72 * time.Hour)
		state := session.snapshot()
		if state.Prompt == nil || state.Prompt.Kind != "actions" {
			t.Fatalf("idle menu closed: %+v", state)
		}
		handler := session.handler(testHost, "known-token")
		rec := request(handler, http.MethodPost, "/api/stop", testHost, "", "known-token", `{"run":1,"close":true}`)
		if rec.Code != http.StatusNoContent || page.Err() == nil {
			t.Fatalf("close at the action menu returned %d", rec.Code)
		}
		if err := <-finished; !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected exit after closing the action menu: %v", err)
		}
	})
}

func TestFailedRunDiagnosticsAreRedactedAndClearedOnReturn(t *testing.T) {
	session := newSession("Fixture", func() {})
	session.Action("deploy")
	session.Phase("preview", "Preview changes with Pulumi")
	session.Protect("fixture-vault-phrase", `fixture"quoted`)
	session.CommandFailure("pulumi preview", errors.New("failed to run"),
		"\x1b[31merror: DNS zone not found\x1b[0m\npassword: plain-password\n"+
			`AWS_SECRET_ACCESS_KEY="new-provider-secret"`+"\n"+
			`Provider setting contained fixture\"quoted`,
		"provider rejected fixture-vault-phrase\nsee https://example.test/?access=private-value")
	writer := session.SignInWriter()
	_, _ = writer.Write([]byte("device sign-in belongs only in the sign-in view\n"))
	session.finish(errors.New("exit status 1"))

	handler := session.handler(testHost, "known-token")
	rec := request(handler, http.MethodGet, "/api/state", testHost, "", "known-token", "")
	var state view
	if err := json.Unmarshal(rec.Body.Bytes(), &state); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("state: %d %v", rec.Code, err)
	}
	for _, part := range []string{"Action: deploy", "Stage: Preview changes with Pulumi", "Error: exit status 1", "pulumi preview", "DNS zone not found", "[Credential line redacted]"} {
		if !strings.Contains(state.Diagnostics, part) {
			t.Fatalf("missing diagnostic %q: %s", part, state.Diagnostics)
		}
	}
	for _, secret := range []string{"fixture-vault-phrase", "fixture\\\"quoted", "plain-password", "new-provider-secret", "private-value", "device sign-in", "\x1b"} {
		if strings.Contains(state.Diagnostics, secret) {
			t.Fatalf("diagnostics exposed %q", secret)
		}
	}
	if !session.returnToActions(state.Revision) || session.snapshot().Diagnostics != "" {
		t.Fatal("diagnostics carried over to the next operation")
	}
}

func TestFormPasswordNeverAppearsInStateOrDiagnostics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		session := newSession("fixture", func() {})
		finished := make(chan error, 1)
		go func() {
			_, _, err := session.AskForm(t.Context(), Prompt{
				Fields:        []Field{{Name: "token", Kind: "password", Value: "must-not-be-published", Saved: true}},
				PreviewFields: []string{"token"},
			})
			finished <- err
		}()
		synctest.Wait()
		state := session.snapshot()
		if state.Prompt == nil || state.Prompt.Fields[0].Value != "" || !state.Prompt.Fields[0].Saved || len(state.Prompt.PreviewFields) != 0 {
			t.Fatal("password field or preview leaked the secret")
		}
		if !session.answer(state.Prompt.ID, reply{Values: map[string]string{"token": "submitted-fixture-token"}}) {
			t.Fatal("secret submission failed")
		}
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		session.finish(errors.New("provider failed with submitted-fixture-token"))
		if state := session.snapshot(); strings.Contains(state.Diagnostics, "submitted-fixture-token") || strings.Contains(state.Error, "submitted-fixture-token") {
			t.Fatal("submitted password appeared in failure diagnostics")
		}
	})
}
