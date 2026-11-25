package wizardweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestVaultPassphraseIdleLock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		passphrase := "test vault passphrase"
		target := "dev"
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		session := newSession("Vault session", cancel)
		defer session.ForgetVault()
		handler := session.handler("example.com", "test-key")
		request := func(method, path, body string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(method, "http://example.com"+path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer test-key")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			return rec
		}
		read := func() view {
			t.Helper()
			rec := request(http.MethodGet, "/api/state", "")
			var state view
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &state) != nil {
				t.Fatalf("invalid wizard state: %s", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), passphrase) {
				t.Fatal("vault passphrase appeared in the browser state")
			}
			return state
		}

		type result struct {
			value string
			err   error
		}
		unlockResult := make(chan result, 1)
		go func() {
			value, err := session.VaultPassphrase(ctx)
			unlockResult <- result{value, err}
		}()
		synctest.Wait()
		first := read().Prompt
		if first == nil || first.Kind != "secret" || !session.answer(first.ID, reply{Value: &passphrase}) {
			t.Fatal("web wizard did not accept the initial passphrase")
		}
		if got := <-unlockResult; got.err != nil || got.value != passphrase {
			t.Fatalf("initial vault unlock failed: %v", got.err)
		}
		session.finish(nil)
		if !session.returnToActions(read().Revision) {
			t.Fatal("could not return to the action menu")
		}
		if value, err := session.VaultPassphrase(ctx); err != nil || value != passphrase || read().Prompt != nil {
			t.Fatalf("next action did not reuse the vault unlock: %v", err)
		}
		session.Note("Current run prompt")
		go func() {
			value, err := session.Ask(ctx, Prompt{Kind: "text", Label: "Choose a target"})
			unlockResult <- result{value, err}
		}()
		synctest.Wait()
		pending := read().Prompt
		if pending == nil {
			t.Fatal("target prompt is missing")
		}

		time.Sleep(19 * time.Minute)
		if rec := request(http.MethodPost, "/api/activity", ""); rec.Code != http.StatusNoContent {
			t.Fatalf("activity returned HTTP %d", rec.Code)
		}
		time.Sleep(19 * time.Minute)
		if read().Locked {
			t.Fatal("wizard locked despite recent browser activity")
		}
		// Background polling at 19 minutes must not extend the deadline.
		time.Sleep(time.Minute)
		synctest.Wait()
		locked := read()
		if !locked.Locked || locked.Prompt != nil || len(locked.Activity) != 0 || session.vault.passphrase != "" {
			t.Fatal("idle vault did not clear the cache and hide the pending operation")
		}
		if session.answer(pending.ID, reply{Value: &target}) {
			t.Fatal("locked wizard accepted an answer to a protected prompt")
		}
		request(http.MethodPost, "/api/activity", "")
		if rec := request(http.MethodPost, "/api/unlock", `{"passphrase":"incorrect"}`); rec.Code != http.StatusUnprocessableEntity || !read().Locked {
			t.Fatal("wrong vault passphrase unlocked the wizard")
		}
		if rec := request(http.MethodPost, "/api/unlock", `{"passphrase":"test vault passphrase"}`); rec.Code != http.StatusNoContent {
			t.Fatalf("valid vault passphrase returned HTTP %d", rec.Code)
		}
		resumed := read()
		if resumed.Locked || resumed.Prompt == nil || resumed.Prompt.ID != pending.ID ||
			!session.answer(pending.ID, reply{Value: &target}) {
			t.Fatal("pending prompt was lost during the idle lock")
		}
		if got := <-unlockResult; got.err != nil || got.value != "dev" {
			t.Fatalf("resumed prompt returned %+v", got)
		}
		time.Sleep(vaultIdleTimeout)
		synctest.Wait()
		if !read().Locked {
			t.Fatal("wizard did not relock after the next idle interval")
		}
	})
}

func TestCreateVaultPasswordInBrowser(t *testing.T) {
	for _, scenario := range []string{"match", "mismatch", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				session := newSession("Create vault", cancel)
				defer session.ForgetVault()
				type result struct {
					pass string
					err  error
				}
				done := make(chan result, 1)
				go func() { pass, err := session.VaultPassphrase(ctx, true); done <- result{pass, err} }()
				synctest.Wait()
				first := session.view.Prompt
				if first == nil || first.Kind != "secret" || first.Label != "Create local vault password" {
					t.Fatal("missing create password prompt")
				}
				password := "new-test-password"
				if !session.answer(first.ID, reply{Value: &password}) {
					t.Fatal("password rejected")
				}
				synctest.Wait()
				confirmation := session.view.Prompt
				if confirmation == nil || confirmation.Kind != "secret" || confirmation.Label != "Confirm new vault password" {
					t.Fatal("missing confirmation")
				}
				raw, _ := json.Marshal(session.view)
				if strings.Contains(string(raw), password) {
					t.Fatal("password leaked into view")
				}
				if session.vault.passphrase != "" {
					t.Fatal("cached before confirmation")
				}
				if scenario == "cancel" {
					cancel()
				} else {
					answer := password
					if scenario == "mismatch" {
						answer = "different"
					}
					session.answer(confirmation.ID, reply{Value: &answer})
				}
				got := <-done
				if scenario != "match" {
					if got.err == nil || session.vault.passphrase != "" {
						t.Fatal("failed setup cached password")
					}
					return
				}
				if got.err != nil || got.pass != password {
					t.Fatal(got.err)
				}
				// Subsequent actions reuse the confirmed password without another setup prompt.
				if got, err := session.VaultPassphrase(ctx, true); err != nil || got != password {
					t.Fatal("confirmed password not reused", err)
				}
			})
		})
	}
}
