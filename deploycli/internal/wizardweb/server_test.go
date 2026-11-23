package wizardweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	session := newSession("test wizard", func() {}, func() {})
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

func TestStopCancelsRunBeforeClosingPage(t *testing.T) {
	run, cancel := context.WithCancel(t.Context())
	defer cancel()
	page, closePage := context.WithCancel(t.Context())
	defer closePage()
	session := newSession("deploy", cancel, closePage)
	session.stop()
	if run.Err() == nil || page.Err() != nil || session.snapshot().Status != "stopping" {
		t.Fatal("stop should cancel the deployment and keep the page available")
	}
	session.finish(run.Err())
	if session.snapshot().Status != "cancelled" {
		t.Fatal("expected a stopped result")
	}
	session.stop()
	if page.Err() == nil {
		t.Fatal("close did not end the browser session")
	}
}

func TestFormAcceptsOnlyEditableFieldsAsOneReply(t *testing.T) {
	session := newSession("test wizard", func() {}, func() {})
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
