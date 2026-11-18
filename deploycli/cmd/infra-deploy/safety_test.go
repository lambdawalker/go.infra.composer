package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPromptStateMachine(t *testing.T) {
	for _, tc := range []struct {
		name, input, def, want string
		choice, fail           bool
	}{
		{"enter", "\n", "dev", "dev", false, false}, {"override", "qa\n", "dev", "qa", false, false}, {"escape", "\x1b", "dev", "", false, true}, {"ctrl-c", "\x03", "dev", "", false, true}, {"ctrl-d", "\x04", "dev", "", false, true}, {"no", "n", "yes", "no", true, false}, {"yes", "y", "no", "yes", true, false}, {"explicit-empty", "\n", "", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan inputEvent, 1)
			events <- inputEvent{data: []byte(tc.input)}
			got, e := askLoop(context.Background(), events, nil, tc.def, tc.choice, func(int, string) {})
			if got != tc.want || (e != nil) != tc.fail {
				t.Fatal(got, e)
			}
		})
	}
	for _, inputErr := range []error{io.EOF, errors.New("broken terminal")} {
		events := make(chan inputEvent, 1)
		events <- inputEvent{err: inputErr}
		ticks := make(chan time.Time, 5)
		for i := 0; i < 5; i++ {
			ticks <- time.Time{}
		}
		if _, e := askLoop(context.Background(), events, ticks, "yes", true, func(int, string) {}); e == nil {
			t.Fatal("input error approved")
		}
	}
	ticks := make(chan time.Time, 5)
	for i := 0; i < 5; i++ {
		ticks <- time.Time{}
	}
	got, e := askLoop(context.Background(), make(chan inputEvent), ticks, "dev", false, func(int, string) {})
	if got != "dev" || e != nil {
		t.Fatal(got, e)
	}
}
func TestExplicitInputCannotTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	events := make(chan inputEvent)
	done := make(chan error, 1)
	go func() { _, e := askLoop(ctx, events, ticks, "", false, func(int, string) {}); done <- e }()
	for i := 0; i < 20; i++ {
		ticks <- time.Time{}
	}
	select {
	case <-done:
		t.Fatal("explicit prompt completed on timer")
	default:
	}
	cancel()
	if e := <-done; e == nil {
		t.Fatal("cancellation approved")
	}
}
func TestPartialTextDisablesDefault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	events := make(chan inputEvent)
	done := make(chan string, 1)
	rendered := make(chan struct{}, 30)
	go func() {
		v, _ := askLoop(ctx, events, ticks, "dev", false, func(int, string) { rendered <- struct{}{} })
		done <- v
	}()
	<-rendered
	events <- inputEvent{data: []byte("qa")}
	<-rendered
	for i := 0; i < 10; i++ {
		ticks <- time.Time{}
	}
	events <- inputEvent{data: []byte("\n")}
	if v := <-done; v != "qa" {
		t.Fatal(v)
	}
}
func TestConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	release, e := operationLock(path)
	if e != nil {
		t.Fatal(e)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if unlock, e := operationLock(path); e == nil {
				wins.Add(1)
				unlock()
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 0 {
		t.Fatal("concurrent writer acquired lock")
	}
	release()
	release, e = operationLock(path)
	if e != nil {
		t.Fatal(e)
	}
	release()
}
func TestExpiredSSOAndCI(t *testing.T) {
	for _, ci := range []bool{true, false} {
		reads, logins := 0, 0
		b, e := authenticatedIdentity(ci, "test", func() ([]byte, error) {
			reads++
			if reads == 1 {
				return nil, errors.New("expired")
			}
			return []byte("identity"), nil
		}, func() error { logins++; return nil })
		if ci {
			if e == nil || logins != 0 {
				t.Fatal("CI invoked SSO")
			}
		} else if e != nil || string(b) != "identity" || logins != 1 {
			t.Fatal(b, e, logins)
		}
	}
}
func TestSavedTargetMismatch(t *testing.T) {
	s := Settings{1, "dev", "sso", "us-east-2", "123", "s3://bucket/prefix"}
	for _, v := range [][3]string{{"456", s.Region, s.Backend}, {s.Account, "us-west-2", s.Backend}, {s.Account, s.Region, "s3://other/prefix"}} {
		if verifySavedTarget(s, v[0], v[1], v[2]) == nil {
			t.Fatal(v)
		}
	}
	if e := verifySavedTarget(s, "", s.Region, s.Backend); e != nil {
		t.Fatal(e)
	}
}
func TestSafeOutputAllowlist(t *testing.T) {
	b, e := safeOutputs([]byte(`{"bucket":"safe","function":"fn","secret":"never","resourceMetadata":{"password":"never"}}`), fixture(t, "ocr").spec.Outputs)
	if e != nil || strings.Contains(string(b), "never") {
		t.Fatal(string(b), e)
	}
}
func TestPrerequisiteBounds(t *testing.T) {
	var active, max atomic.Int32
	jobs := make([]func() error, 8)
	for i := range jobs {
		jobs[i] = func() error {
			n := active.Add(1)
			for old := max.Load(); n > old; old = max.Load() {
				if max.CompareAndSwap(old, n) {
					break
				}
			}
			active.Add(-1)
			return nil
		}
	}
	if e := prerequisites(context.Background(), 2, jobs); e != nil || max.Load() > 2 {
		t.Fatal(max.Load(), e)
	}
	jobs[2] = func() error { return errors.New("failed") }
	if prerequisites(context.Background(), 2, jobs) == nil {
		t.Fatal("failure lost")
	}
}
func TestBuildLockStopsConcurrentPublication(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dist", "ocr-bench")
	os.MkdirAll(dir, 0700)
	release, e := operationLock(filepath.Join(dir, "build.lock"))
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if _, e := fixture(t, "ocr").build(context.Background(), root, false); e == nil {
		t.Fatal("build ignored active writer")
	}
}

func TestPortableSettingsScopeAndNoSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portable.json")
	s := Settings{1, "dev", "sso", "us-east-2", "123456789012", "s3://state/prefix"}
	if e := exportSettings(path, "project", s); e != nil {
		t.Fatal(e)
	}
	got, e := importSettings(path, "project", "dev")
	if e != nil || got != s {
		t.Fatal(got, e)
	}
	if _, e = importSettings(path, "project", "qa"); e == nil {
		t.Fatal("cross-environment import accepted")
	}
	if _, e = importSettings(path, "other", "dev"); e == nil {
		t.Fatal("cross-project import accepted")
	}
}
func TestStateTLSValidation(t *testing.T) {
	good := `{"Policy":"{\"Statement\":[{\"Effect\":\"Deny\",\"Principal\":\"*\",\"Action\":\"s3:*\",\"Resource\":[\"arn:aws:s3:::state\",\"arn:aws:s3:::state/*\"],\"Condition\":{\"Bool\":{\"aws:SecureTransport\":\"false\"}}}]}"}`
	if e := requireStateTLS([]byte(good), "state"); e != nil {
		t.Fatal(e)
	}
	if requireStateTLS([]byte(good), "other") == nil {
		t.Fatal("wrong bucket accepted")
	}
	if requireStateTLS([]byte(`{"Policy":"{\"Statement\":[]}"}`), "state") == nil {
		t.Fatal("missing TLS enforcement accepted")
	}
}

func TestRemoteStackIdentity(t *testing.T) {
	known := []byte(`{"deployment":{"resources":[{"type":"pulumi:pulumi:Stack","outputs":{"deploymentIdentity":"original-dev"}},{"type":"aws:s3/bucket:Bucket"}]}}`)
	if e := verifyStackIdentity(known, "original", "dev", false); e != nil {
		t.Fatal(e)
	}
	if verifyStackIdentity(known, "other", "dev", true) == nil {
		t.Fatal("explicit adoption overrode known identity")
	}
	legacy := []byte(`{"deployment":{"resources":[{"type":"aws:s3/bucket:Bucket"}]}}`)
	if verifyStackIdentity(legacy, "original", "dev", false) == nil {
		t.Fatal("legacy adopted implicitly")
	}
	if e := verifyStackIdentity(legacy, "original", "dev", true); e != nil {
		t.Fatal(e)
	}
}
