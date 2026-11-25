package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

func TestWizardIndexTracksConcurrentProjectListeners(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	index := filepath.Join(home, ".infra-composer", "_wizards")
	firstRoot, secondRoot := t.TempDir(), t.TempDir()
	firstCLI, secondCLI := fixture(t, "email"), fixture(t, "ocr")
	if _, _, _, err := firstCLI.projectPaths(t.Context(), firstRoot, "email-demo", false); err != nil {
		t.Fatal(err)
	}

	type runningWizard struct {
		path string
		stop func()
	}
	start := func(cli deployCLI, root, project string, chooseProject <-chan struct{}, saved chan<- error) runningWizard {
		t.Helper()
		ctx, cancel := context.WithCancel(t.Context())
		registered := make(chan string, 1)
		exited := make(chan struct{})
		var serveErr error
		var registration *wizardRegistration
		go func() {
			defer close(exited)
			serveErr = wizardweb.Serve(ctx, cli.spec.Title, cli.spec.Repository, false, func(port int, instanceID string) (func() error, error) {
				var err error
				registration, err = registerWizard(root, cli.spec.Repository, "", port, instanceID)
				if err != nil {
					return nil, err
				}
				registered <- registration.path
				return registration.remove, nil
			}, func(ctx context.Context, _ *wizardweb.Session, _ bool) error {
				select {
				case <-chooseProject:
					ctx = context.WithValue(ctx, wizardRegistrationKey{}, registration)
					_, _, _, err := cli.projectPaths(ctx, root, project, false)
					saved <- err
				case <-ctx.Done():
				}
				<-ctx.Done()
				return ctx.Err()
			})
		}()
		stop := sync.OnceFunc(func() {
			t.Helper()
			cancel()
			select {
			case <-exited:
				if serveErr != nil {
					t.Error(serveErr)
				}
			case <-time.After(10 * time.Second):
				t.Error("wizard did not close")
			}
		})
		t.Cleanup(stop)
		select {
		case path := <-registered:
			return runningWizard{path: path, stop: stop}
		case <-exited:
			t.Fatalf("wizard exited before registering: %v", serveErr)
		case <-time.After(10 * time.Second):
			t.Fatal("wizard did not register")
		}
		return runningWizard{}
	}
	read := func(path string) wizardInstance {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record wizardInstance
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		if record.Version != 1 || record.Port < 1 || record.PID != os.Getpid() || record.StartedAt.IsZero() ||
			filepath.Dir(path) != index || filepath.Base(path) != record.InstanceID+".json" {
			t.Fatalf("invalid wizard index entry: %+v", record)
		}
		return record
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{}}
	t.Cleanup(client.CloseIdleConnections)
	checkListener := func(record wizardInstance) {
		t.Helper()
		base := fmt.Sprintf("http://127.0.0.1:%d", record.Port)
		response, err := client.Head(base + "/")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK || response.Header.Get("X-Infra-Composer-Instance") != record.InstanceID {
			t.Fatalf("index entry did not match the listener: %d %q", response.StatusCode, response.Header.Get("X-Infra-Composer-Instance"))
		}
		response, err = client.Get(base + "/api/state")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("index metadata granted API access: %d", response.StatusCode)
		}
	}

	choose, saved := make(chan struct{}), make(chan error, 1)
	first := start(firstCLI, firstRoot, "", nil, nil)
	second := start(secondCLI, secondRoot, "ocr-demo", choose, saved)
	email, ocr := read(first.path), read(second.path)
	if email.Port == ocr.Port || email.InstanceID == ocr.InstanceID ||
		email.Repository != firstCLI.spec.Repository || ocr.Repository != secondCLI.spec.Repository ||
		email.Project != "email-demo" || ocr.Project != "" {
		t.Fatalf("wizard index did not isolate the instances: %+v and %+v", email, ocr)
	}
	checkListener(email)
	checkListener(ocr)

	close(choose)
	select {
	case err := <-saved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("wizard did not save the selected project")
	}
	ocr.Project = "ocr-demo"
	if updated := read(second.path); updated != ocr {
		t.Fatalf("project setup changed more than the instance's project: %+v", updated)
	}
	first.stop()
	if _, err := os.Stat(first.path); !os.IsNotExist(err) {
		t.Fatalf("first wizard entry was not removed: %v", err)
	}
	checkListener(read(second.path))
	second.stop()
	entries, err := os.ReadDir(index)
	if err != nil || len(entries) != 0 {
		t.Fatalf("index after both wizards closed: %v, %v", entries, err)
	}
}
