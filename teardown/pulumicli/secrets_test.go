package pulumicli

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/lambdawalker/go.infra.composer/teardown"
)

const secretARN = "arn:aws:secretsmanager:us-east-1:123456789012:secret:example/auth-Ab12Cd"

func TestSecretsExportProbesParentWithoutValues(t *testing.T) {
	fixture, err := os.ReadFile("testdata/secrets-export.json")
	if err != nil {
		t.Fatal(err)
	}
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if n != "pulumi" {
			t.Fatalf("unexpected command: %s %v", n, a)
		}
		if a[1] == "ls" {
			return []byte(`[{"name":"dev"}]`), nil
		}
		return fixture, nil
	})
	artifacts, err := d.Inventory(context.Background(), target(cfg().Projects[0], "dev"))
	if err != nil || len(artifacts) != 2 {
		t.Fatal(artifacts, err)
	}
	for _, a := range artifacts {
		if a.Kind != "secret" || a.ID != secretARN {
			t.Fatalf("parent not tracked: %#v", a)
		}
	}
	data, _ := json.Marshal(artifacts)
	if strings.Contains(string(data), "DO_NOT_PERSIST") || strings.Contains(string(data), "version-identifier") {
		t.Fatal(string(data))
	}
	for _, c := range f.calls {
		if strings.Contains(c, "get-secret-value") {
			t.Fatal(c)
		}
	}
}

func TestSecretInspectionRequiresAuthoritativeParentEvidence(t *testing.T) {
	present := `{"ARN":"` + secretARN + `","Name":"example/auth"}`
	for _, tt := range []struct {
		name, body, code string
		want             teardown.State
		wantErr          bool
	}{
		{name: "present", body: present, want: teardown.StatePresent},
		{name: "scheduled", body: strings.TrimSuffix(present, "}") + `,"DeletedDate":"2026-11-09T12:00:00Z"}`, want: teardown.StatePending},
		{name: "scheduled epoch", body: strings.TrimSuffix(present, "}") + `,"DeletedDate":1794225600}`, want: teardown.StatePending},
		{name: "absent", code: "ResourceNotFoundException", want: teardown.StateAbsent},
		{name: "denied", code: "AccessDeniedException", wantErr: true},
		{name: "empty", body: `{}`, wantErr: true},
		{name: "missing name", body: `{"ARN":"` + secretARN + `"}`, wantErr: true},
		{name: "wrong name", body: `{"ARN":"` + secretARN + `","Name":"other"}`, wantErr: true},
		{name: "wrong ARN", body: `{"ARN":"arn:aws:secretsmanager:us-east-1:123456789012:secret:other-Ab12Cd","Name":"other"}`, wantErr: true},
		{name: "invalid date", body: strings.TrimSuffix(present, "}") + `,"DeletedDate":"invalid"}`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
				if n != "aws" || !reflect.DeepEqual(a, []string{"secretsmanager", "describe-secret", "--secret-id", secretARN, "--output", "json", "--no-cli-pager"}) {
					t.Fatalf("unexpected command: %s %v", n, a)
				}
				if tt.code != "" {
					return errCLI("An error occurred (" + tt.code + ") when calling the DescribeSecret operation: DO_NOT_PERSIST")
				}
				return []byte(tt.body), nil
			})
			state, err := d.Inspect(context.Background(), teardown.Artifact{ID: secretARN, Kind: "secret", Region: "us-east-1"})
			if (err != nil) != tt.wantErr || state != tt.want {
				t.Fatal(state, err)
			}
			if err != nil && strings.Contains(err.Error(), "DO_NOT_PERSIST") {
				t.Fatal(err)
			}
		})
	}
}

func TestSecretInspectionRejectsUnsafeIdentifiersBeforeDescribe(t *testing.T) {
	for _, id := range []string{"example/auth", "arn:aws:secretsmanager:us-east-1:999999999999:secret:example/auth-Ab12Cd", "arn:aws:s3:us-east-1:123456789012:secret:example/auth-Ab12Cd", "arn:aws:secretsmanager::123456789012:secret:example/auth-Ab12Cd", "arn:aws:secretsmanager:us-east-1:123456789012:secret:"} {
		t.Run(id, func(t *testing.T) {
			d, _ := newFake(t, func(n string, a []string) ([]byte, error) { t.Fatalf("unsafe describe: %s %v", n, a); return nil, nil })
			if _, err := d.Inspect(context.Background(), teardown.Artifact{ID: id, Kind: "secret", Region: "us-east-1"}); err == nil {
				t.Fatal("unsafe identifier accepted")
			}
		})
	}
}

func TestRetainedSecretVersionIsAbsentOnlyAfterParentDisappears(t *testing.T) {
	fixture, err := os.ReadFile("testdata/secrets-export.json")
	if err != nil {
		t.Fatal(err)
	}
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		if n == "pulumi" {
			if a[1] == "ls" {
				return []byte(`[{"name":"dev"}]`), nil
			}
			return fixture, nil
		}
		if a[0] != "secretsmanager" || a[1] != "describe-secret" || a[3] != secretARN {
			t.Fatal(n, a)
		}
		return errCLI("An error occurred (ResourceNotFoundException) when calling the DescribeSecret operation: gone")
	})
	artifacts, err := d.Inventory(context.Background(), target(cfg().Projects[0], "dev"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := d.Inspect(context.Background(), artifacts[1])
	if err != nil || state != teardown.StateAbsent {
		t.Fatal(state, err)
	}
}

func TestScheduledSecretBlocksRecoveryAndRemainsEligibleForFastCheck(t *testing.T) {
	fixture, err := os.ReadFile("testdata/secrets-export.json")
	if err != nil {
		t.Fatal(err)
	}
	destroyed, removed, gone := false, false, false
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if n == "pulumi" {
			switch a[0] + " " + a[1] {
			case "stack ls":
				if removed {
					return []byte(`[]`), nil
				}
				return []byte(`[{"name":"dev"}]`), nil
			case "stack export":
				if destroyed {
					return []byte(`{"deployment":{"resources":[]}}`), nil
				}
				return fixture, nil
			case "stack import", "refresh --stack":
				return nil, nil
			case "destroy --stack":
				destroyed = true
				return nil, nil
			case "stack rm":
				removed = true
				return nil, nil
			}
		}
		if n == "aws" && a[0] == "secretsmanager" && a[1] == "describe-secret" {
			if gone {
				return errCLI("An error occurred (ResourceNotFoundException) when calling the DescribeSecret operation: gone")
			}
			return []byte(`{"ARN":"` + secretARN + `","Name":"example/auth","DeletedDate":"2026-12-02T00:00:00Z"}`), nil
		}
		if n == "aws" && a[0] == "s3api" && a[1] == "get-bucket-location" {
			return errCLI("An error occurred (NoSuchBucket) when calling the GetBucketLocation operation: gone")
		}
		t.Fatalf("unexpected cloud/recovery command: %s %v", n, a)
		return nil, nil
	})
	engine := teardown.Engine{Scope: d.Config.Scope, Driver: d, Store: teardown.FileStore{Path: t.TempDir() + "/report.json"}, Recovery: []teardown.Artifact{recovery("recovery_bucket", "state")}}
	report, err := engine.Nuke(context.Background())
	if err == nil {
		t.Fatal("pending secret accepted as complete")
	}
	secrets := 0
	for _, result := range report.Results {
		if result.Artifact.Kind == "secret" {
			secrets++
			if result.Status != teardown.Remains {
				t.Fatal(result)
			}
		}
		if result.Artifact.Recovery && result.Status != teardown.Retained {
			t.Fatal(result)
		}
	}
	if secrets != 1 {
		t.Fatalf("expected consolidated parent, got %d", secrets)
	}
	// A full check seeds fast-check evidence; checks never delete recovery data.
	report, err = engine.Check(context.Background(), false)
	if err == nil {
		t.Fatal("scheduled secret accepted by full check")
	}
	gone = true
	report, err = engine.Check(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range report.Checks {
		if result.Artifact.Kind == "secret" && result.Status != teardown.Deleted && result.Status != teardown.Absent {
			t.Fatal(result)
		}
	}
	for _, call := range f.calls {
		if strings.Contains(call, "get-secret-value") || strings.Contains(call, "delete-secret") {
			t.Fatal(call)
		}
	}
}
