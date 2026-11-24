package wizardweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

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
				s.Preview([]Change{{Resource: "fixture", Operation: "create"}}, 1)
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
			state.Action != "" || state.Error != "" || len(state.Activity) != 0 || len(state.Visited) != 0 {
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
