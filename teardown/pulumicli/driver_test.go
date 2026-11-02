package pulumicli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/lambdawalker/go.infra.composer/teardown"
)

type fakeRunner struct {
	t     *testing.T
	calls []string
	envs  [][]string
	run   func(string, []string) ([]byte, error)
}

func (f *fakeRunner) Run(_ context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	f.envs = append(f.envs, append([]string(nil), env...))
	if name == "pulumi" {
		if dir != "/project" {
			f.t.Fatalf("unexpected working directory %q", dir)
		}
		if !strings.Contains(strings.Join(env, "\n"), "PULUMI_BACKEND_URL=s3://state") {
			f.t.Fatal("backend not scoped")
		}
	}
	return f.run(name, args)
}
func cfg() Config {
	return Config{Scope: teardown.Scope{Application: "example", Account: "123456789012"}, Projects: []Project{{Name: "app", Directory: "/project", Backend: "s3://state", Region: "us-east-1"}}}
}
func newFake(t *testing.T, fn func(string, []string) ([]byte, error)) (*Driver, *fakeRunner) {
	f := &fakeRunner{t: t}
	f.run = func(n string, a []string) ([]byte, error) {
		if n == "aws" && a[0] == "sts" {
			return []byte(`{"Account":"123456789012"}`), nil
		}
		return fn(n, a)
	}
	d, e := New(cfg(), f)
	if e != nil {
		t.Fatal(e)
	}
	return d, f
}
func errCLI(s string) ([]byte, error) { return []byte(s), errors.New("exit status 1") }
func TestDiscoveryListsEveryStack(t *testing.T) {
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if n != "pulumi" || !reflect.DeepEqual(a, []string{"stack", "ls", "--json", "--non-interactive", "--project", "app"}) {
			t.Fatal(n, a)
		}
		return []byte(`[{"name":"org/app/dev"},{"name":"prod"}]`), nil
	})
	ts, e := d.Discover(context.Background(), d.Config.Scope)
	if e != nil || len(ts) != 2 || ts[0].Stack != "org/app/dev" || ts[1].ID != "s3://state|app|prod" {
		t.Fatal(ts, e)
	}
	if len(f.calls) != 2 {
		t.Fatal(f.calls)
	}
}
func TestAccountMismatchPreventsAllFurtherOperations(t *testing.T) {
	f := &fakeRunner{t: t, run: func(n string, a []string) ([]byte, error) { return []byte(`{"Account":"999999999999"}`), nil }}
	d, _ := New(cfg(), f)
	if _, e := d.Inspect(context.Background(), teardown.Artifact{ID: "x", Kind: "bucket"}); e == nil {
		t.Fatal("account mismatch accepted")
	}
	if len(f.calls) != 1 {
		t.Fatal(f.calls)
	}
}
func TestInventoryCapturesIdentifiersOnly(t *testing.T) {
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		if a[1] == "ls" {
			return []byte(`[{"name":"dev"}]`), nil
		}
		return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::aws:lambda/function:Function::f","custom":true,"type":"aws:lambda/function:Function","id":"fn","inputs":{"environment":{"secret":"DO_NOT_PERSIST"}},"outputs":{"secret":{"ciphertext":"DO_NOT_PERSIST"}}}]}}`), nil
	})
	as, e := d.Inventory(context.Background(), target(cfg().Projects[0], "dev"))
	if e != nil || len(as) != 2 || as[0].Kind != "lambda" || as[0].ID != "fn" {
		t.Fatal(as, e)
	}
	b, _ := json.Marshal(as)
	if strings.Contains(string(b), "DO_NOT_PERSIST") {
		t.Fatal(string(b))
	}
}
func TestOwnershipRejectsForeignStateBeforeMutation(t *testing.T) {
	for _, resource := range []string{
		`{"urn":"urn:pulumi:dev::foreign::aws:s3/bucket:Bucket::b","custom":true,"type":"aws:s3/bucket:Bucket","id":"b"}`,
		`{"urn":"urn:pulumi:dev::app::pulumi:providers:aws::p","custom":true,"type":"pulumi:providers:aws","inputs":{"profile":"another"}}`,
		`{"urn":"urn:pulumi:dev::app::pulumi:providers:aws::p","custom":true,"type":"pulumi:providers:aws","inputs":{"region":"invalid-region"}}`,
		`{"urn":"urn:pulumi:dev::app::aws:s3/bucket:Bucket::b","custom":true,"type":"aws:s3/bucket:Bucket","outputs":{"arn":"arn:aws:s3:us-east-1:999999999999:bucket/b"}}`,
		`{"urn":"urn:pulumi:dev::app::aws:s3/bucket:Bucket::b","custom":true,"type":"aws:s3/bucket:Bucket","inputs":{"tags":{"ic:application":"other"}}}`,
	} {
		t.Run(resource, func(t *testing.T) {
			d, f := newFake(t, func(n string, a []string) ([]byte, error) {
				if a[1] == "ls" {
					return []byte(`[{"name":"dev"}]`), nil
				}
				return []byte(`{"deployment":{"resources":[` + resource + `]}}`), nil
			})
			e := d.Destroy(context.Background(), target(cfg().Projects[0], "dev"))
			if e == nil {
				t.Fatal("foreign state accepted")
			}
			for _, c := range f.calls {
				if strings.Contains(c, "refresh") || strings.Contains(c, "destroy") {
					t.Fatal(c)
				}
			}
		})
	}
}
func TestAuthoritativeStackAbsenceAndDeniedAreDifferent(t *testing.T) {
	for _, denied := range []bool{false, true} {
		d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
			if denied {
				return errCLI("private secret access denied")
			}
			return []byte(`[]`), nil
		})
		a := teardown.Artifact{ID: "s3://state|app|dev", TargetID: "s3://state|app|dev", Kind: "stack"}
		s, e := d.Inspect(context.Background(), a)
		if denied {
			if e == nil || strings.Contains(e.Error(), "private") {
				t.Fatal(s, e)
			}
		} else if e != nil || s != teardown.StateAbsent {
			t.Fatal(s, e)
		}
	}
}
func TestAWSAbsenceRequiresSpecificErrorCode(t *testing.T) {
	for _, code := range []string{"ResourceNotFoundException", "AccessDeniedException", "ThrottlingException"} {
		d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
			return errCLI("An error occurred (" + code + ") when calling the GetFunctionConfiguration operation: SECRET")
		})
		s, e := d.Inspect(context.Background(), teardown.Artifact{ID: "f", Kind: "lambda", Region: "us-east-1"})
		if code == "ResourceNotFoundException" {
			if e != nil || s != teardown.StateAbsent {
				t.Fatal(s, e)
			}
		} else if e == nil || strings.Contains(e.Error(), "SECRET") {
			t.Fatal(s, e)
		}
	}
}
func TestGuardRewritePreservesEncryptedEnvelopeAndExternal(t *testing.T) {
	b := []byte(`{"version":3,"deployment":{"secrets_providers":{"type":"passphrase","state":{"salt":"encrypted"}},"resources":[{"urn":"managed","protect":true,"retainOnDelete":true,"outputs":{"secret":{"ciphertext":"keep"}}},{"urn":"external","external":true,"protect":true,"retainOnDelete":true}]}}`)
	got, changed, e := guards(b)
	if e != nil || !changed {
		t.Fatal(e)
	}
	var x map[string]any
	_ = json.Unmarshal(got, &x)
	dep := x["deployment"].(map[string]any)
	rs := dep["resources"].([]any)
	if rs[0].(map[string]any)["protect"] != false || rs[1].(map[string]any)["protect"] != true || !strings.Contains(string(got), `"ciphertext":"keep"`) || !strings.Contains(string(got), `"salt":"encrypted"`) {
		t.Fatal(string(got))
	}
}
func TestDestroyUsesEncryptedStagingThenEmptyStateRemoval(t *testing.T) {
	destroyed := false
	imported := false
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		switch a[0] + " " + a[1] {
		case "stack ls":
			return []byte(`[{"name":"dev"}]`), nil
		case "stack export":
			if destroyed {
				return []byte(`{"deployment":{"resources":[]}}`), nil
			}
			return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::aws:lambda/function:Function::fn","custom":true,"type":"aws:lambda/function:Function","id":"fn","protect":true,"outputs":{"secret":{"ciphertext":"keep"}}}]}}`), nil
		case "stack import":
			var path string
			for i, v := range a {
				if v == "--file" {
					path = a[i+1]
				}
			}
			b, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0600 || !strings.Contains(string(b), `"ciphertext":"keep"`) {
				t.Fatal("bad staging")
			}
			imported = true
			return nil, nil
		case "destroy --stack":
			if !imported {
				t.Fatal("no import")
			}
			if !strings.Contains(strings.Join(a, " "), "--continue-on-error") {
				t.Fatal(a)
			}
			destroyed = true
			return nil, nil
		case "stack rm":
			if !destroyed {
				t.Fatal("premature removal")
			}
			return nil, nil
		default:
			if a[0] == "refresh" {
				return errCLI("refresh failed SECRET")
			}
			t.Fatal(n, a)
			return nil, nil
		}
	})
	if e := d.Destroy(context.Background(), target(cfg().Projects[0], "dev")); e != nil {
		t.Fatal(e)
	}
}
func TestUnsupportedAndIncompleteResponsesRemainUnverified(t *testing.T) {
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) { return []byte(`{}`), nil })
	for _, a := range []teardown.Artifact{{ID: "x", Kind: "unsupported"}, {ID: "x", Kind: "alarm"}, {ID: "x", Kind: "ses"}} {
		if s, e := d.Inspect(context.Background(), a); e == nil || s == teardown.StateAbsent {
			t.Fatal(a, s, e)
		}
	}
}
func recovery(kind, id string) teardown.Artifact {
	return teardown.Artifact{ID: id, Kind: kind, Region: "us-east-1", Recovery: true, Data: map[string]string{"exclusive": "true"}}
}
func TestRecoveryPreflightBlocksAllMutationForForeignObjects(t *testing.T) {
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if n == "pulumi" {
			return []byte(`[]`), nil
		}
		if a[1] == "list-object-versions" {
			return []byte(`{"Versions":[{"Key":"foreign/secret","VersionId":"v1"}]}`), nil
		}
		t.Fatal(n, a)
		return nil, nil
	})
	a := recovery("recovery_bucket", "state")
	d.Config.Recovery = []teardown.Artifact{a}
	if e := d.PrepareRecovery(context.Background(), d.Config.Recovery); e == nil {
		t.Fatal("foreign object accepted")
	}
	if e := d.RemoveRecovery(context.Background(), a); e == nil {
		t.Fatal("missing preflight accepted")
	}
	for _, c := range f.calls {
		if strings.Contains(c, "delete-") {
			t.Fatal(c)
		}
	}
}
func TestRecoveryKMSPendingIsNotAbsence(t *testing.T) {
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		return []byte(`{"KeyMetadata":{"KeyState":"PendingDeletion"}}`), nil
	})
	s, e := d.Inspect(context.Background(), recovery("recovery_key", "key"))
	if e != nil || s != teardown.StatePending {
		t.Fatal(s, e)
	}
}
func TestRecoveryRejectsAWSManagedKey(t *testing.T) {
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if n == "pulumi" {
			return []byte(`[]`), nil
		}
		return []byte(`{"KeyMetadata":{"AWSAccountId":"123456789012","KeyManager":"AWS","KeyState":"Enabled"}}`), nil
	})
	a := recovery("recovery_key", "key")
	d.Config.Recovery = []teardown.Artifact{a}
	if e := d.PrepareRecovery(context.Background(), d.Config.Recovery); e == nil {
		t.Fatal("AWS-managed key accepted")
	}
	for _, c := range f.calls {
		if strings.Contains(c, "schedule") {
			t.Fatal(c)
		}
	}
}
func TestPurgeChecksPerObjectErrors(t *testing.T) {
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		if a[1] == "list-object-versions" {
			return []byte(`{"Versions":[{"Key":"a","VersionId":"null"}],"DeleteMarkers":[{"Key":"a","VersionId":"v2"}]}`), nil
		}
		if a[1] == "delete-objects" {
			if !strings.Contains(strings.Join(a, " "), `"VersionId":"v2"`) {
				t.Fatal(a)
			}
			return []byte(`{"Errors":[{"Code":"AccessDenied"}]}`), nil
		}
		t.Fatal(a)
		return nil, nil
	})
	if e := d.purge(context.Background(), teardown.Artifact{ID: "b", Region: "us-east-1"}); e == nil {
		t.Fatal("object deletion error ignored")
	}
}
func TestVersionsFollowsBothPaginationMarkers(t *testing.T) {
	pages := 0
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		pages++
		if pages == 1 {
			return []byte(`{"IsTruncated":true,"NextKeyMarker":"a","NextVersionIdMarker":"v1","Versions":[{"Key":"a","VersionId":"v1"}]}`), nil
		}
		joined := strings.Join(a, " ")
		if !strings.Contains(joined, "--key-marker a --version-id-marker v1") {
			t.Fatal(a)
		}
		return []byte(`{"Versions":[{"Key":"a","VersionId":"v2"}]}`), nil
	})
	vs, e := d.versions(context.Background(), teardown.Artifact{ID: "bucket"})
	if e != nil || len(vs) != 2 || pages != 2 {
		t.Fatal(vs, e, pages)
	}
}

func TestProviderDefaultsAndAWSManagedPolicyAllowed(t *testing.T) {
	d, _ := newFake(t, func(string, []string) ([]byte, error) { return nil, nil })
	rs := []resource{{URN: "urn:pulumi:dev::app::pulumi:providers:aws::default", Type: "pulumi:providers:aws", Inputs: map[string]json.RawMessage{"skipCredentialsValidation": json.RawMessage(`"false"`), "skipRequestingAccountId": json.RawMessage(`false`), "endpoints": json.RawMessage(`[]`)}}, {URN: "urn:pulumi:dev::app::aws:iam/rolePolicyAttachment:RolePolicyAttachment::p", Type: "aws:iam/rolePolicyAttachment:RolePolicyAttachment", Inputs: map[string]json.RawMessage{"policyArn": json.RawMessage(`"arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"`)}}}
	if e := d.ownership(target(cfg().Projects[0], "dev"), rs); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"accessKey", "secretKey", "token", "assumeRole", "assumeRoles", "profile", "endpoints"} {
		rs[0].Inputs[key] = json.RawMessage(`"secret"`)
		if e := d.ownership(target(cfg().Projects[0], "dev"), rs); e == nil {
			t.Fatalf("accepted %s", key)
		}
		delete(rs[0].Inputs, key)
	}
}
func TestMissingBackendRequiresAuthoritativeHead(t *testing.T) {
	for _, code := range []string{"404", "AccessDenied"} {
		d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
			if n == "pulumi" {
				return errCLI("backend inaccessible SECRET")
			}
			if a[1] != "head-bucket" || !strings.Contains(strings.Join(a, " "), "--expected-bucket-owner 123456789012") {
				t.Fatal(a)
			}
			return errCLI("An error occurred (" + code + ") when calling the HeadBucket operation: SECRET")
		})
		s, e := d.Inspect(context.Background(), teardown.Artifact{ID: "s3://state|app|dev", Kind: "stack"})
		if code == "404" {
			if s != teardown.StateAbsent || e != nil {
				t.Fatal(s, e)
			}
		} else if e == nil || s == teardown.StateAbsent || strings.Contains(e.Error(), "SECRET") {
			t.Fatal(s, e)
		}
	}
}
func TestRecoveryRecognizesOnlyConfiguredProjectLayouts(t *testing.T) {
	d, _ := newFake(t, func(string, []string) ([]byte, error) { return nil, nil })
	for _, key := range []string{".pulumi/stacks/app/dev.json", ".pulumi/history/organization/app/dev/123.json", ".pulumi/backups/organization/app/dev/123.json", ".pulumi/locks/app/dev/123.json", ".pulumi/meta.yaml"} {
		if !d.recognizedKey(key) {
			t.Fatal(key)
		}
	}
	for _, key := range []string{".pulumi/stacks/foreign/dev.json", ".pulumi/history/organization/foreign/dev/123.json", "personal.txt", ".pulumi/stacks/organization/app"} {
		if d.recognizedKey(key) {
			t.Fatal(key)
		}
	}
}
func TestPrepFailureDoesNotPreventDestroy(t *testing.T) {
	destroyed := false
	exports := 0
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		if n == "aws" {
			return errCLI("An error occurred (AccessDenied) when calling the UpdateTable operation: SECRET")
		}
		if a[0] == "refresh" {
			return nil, nil
		}
		switch a[1] {
		case "ls":
			return []byte(`[{"name":"dev"}]`), nil
		case "export":
			exports++
			if exports > 2 {
				return []byte(`{"deployment":{"resources":[]}}`), nil
			}
			return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::aws:dynamodb/table:Table::t","custom":true,"type":"aws:dynamodb/table:Table","id":"table"}]}}`), nil
		case "--stack":
			destroyed = true
			return nil, nil
		case "rm":
			return nil, nil
		}
		t.Fatal(n, a)
		return nil, nil
	})
	e := d.Destroy(context.Background(), target(cfg().Projects[0], "dev"))
	if !destroyed || e == nil || strings.Contains(e.Error(), "SECRET") {
		t.Fatal(destroyed, e)
	}
}
func TestDNSProbeUsesServicePagination(t *testing.T) {
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		s := strings.Join(a, " ")
		if !strings.Contains(s, `"MaxItems":"1"`) || !strings.Contains(s, "--no-paginate") || strings.Contains(s, "--max-items") {
			t.Fatal(a)
		}
		return []byte(`{"ResourceRecordSets":[{"Name":"other.example.","Type":"A"}]}`), nil
	})
	s, e := d.Inspect(context.Background(), teardown.Artifact{Kind: "record", ID: "app.example.", Data: map[string]string{"zone": "Z123", "type": "A"}})
	if e != nil || s != teardown.StateAbsent {
		t.Fatal(s, e)
	}
}
func TestAuxiliarySnapshotBackupsAndLambdaLogs(t *testing.T) {
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		if a[0] != "dynamodb" || a[1] != "list-backups" {
			t.Fatal(a)
		}
		return []byte(`{"BackupSummaries":[{"BackupArn":"arn:aws:dynamodb:us-east-1:123456789012:table/table/backup/one","TableName":"table","BackupType":"USER"},{"BackupArn":"arn:aws:dynamodb:us-east-1:123456789012:table/table/backup/two","TableName":"table","BackupType":"AWS_BACKUP"}]}`), nil
	})
	tgt := target(cfg().Projects[0], "dev")
	as, e := d.auxiliary(context.Background(), tgt, resource{Type: "aws:dynamodb/table:Table", ID: "table"})
	if e != nil || len(as) != 4 || as[0].Kind != "table_backups" || as[1].Kind != "aws_backup_collection" || as[2].Kind != "dynamodb_backup" || as[3].Kind != "aws_backup" || as[0].Data["parent_id"] != "table" {
		t.Fatal(as, e)
	}
	as, e = d.auxiliary(context.Background(), tgt, resource{Type: "aws:lambda/function:Function", ID: "arn:aws:lambda:us-east-1:123456789012:function:fn"})
	if e != nil || len(as) != 1 || as[0].ID != "/aws/lambda/fn" {
		t.Fatal(as, e)
	}
}
func TestAuxiliaryCleanupWaitsForParentAbsence(t *testing.T) {
	for _, parentAbsent := range []bool{false, true} {
		deleted := false
		d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
			switch a[1] {
			case "get-function-configuration":
				if parentAbsent {
					return errCLI("An error occurred (ResourceNotFoundException) when calling the GetFunctionConfiguration operation: missing")
				}
				return []byte(`{"FunctionName":"fn"}`), nil
			case "describe-log-groups":
				return []byte(`{"logGroups":[{"logGroupName":"/aws/lambda/fn"},{"logGroupName":"/aws/lambda/fn-extra"}]}`), nil
			case "delete-log-group":
				deleted = true
				return []byte(`{}`), nil
			}
			t.Fatal(a)
			return nil, nil
		})
		data := routing(target(cfg().Projects[0], "dev"))
		data["cleanup"] = "true"
		data["parent_kind"] = "lambda"
		data["parent_id"] = "fn"
		a := teardown.Artifact{ID: "/aws/lambda/fn", Kind: "log_group", Region: "us-east-1", TargetID: "s3://state|app|dev", Data: data}
		e := d.Cleanup(context.Background(), a)
		if deleted != parentAbsent || (e == nil) != parentAbsent {
			t.Fatal(deleted, parentAbsent, e)
		}
	}
}
func TestBackupInspectPendingAndAbsent(t *testing.T) {
	for _, status := range []string{"AVAILABLE", "DELETING", "DELETED"} {
		d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
			return []byte(`{"BackupDescription":{"BackupDetails":{"BackupStatus":"` + status + `"}}}`), nil
		})
		s, e := d.Inspect(context.Background(), teardown.Artifact{ID: "arn:aws:dynamodb:us-east-1:123456789012:table/t/backup/1", Kind: "dynamodb_backup"})
		want := map[string]teardown.State{"AVAILABLE": teardown.StatePresent, "DELETING": teardown.StatePending, "DELETED": teardown.StateAbsent}[status]
		if e != nil || s != want {
			t.Fatal(s, e)
		}
	}
}
func TestTargetCannotDeleteItsRecoveryBackend(t *testing.T) {
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if a[1] == "ls" {
			return []byte(`[{"name":"dev"}]`), nil
		}
		return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::aws:s3/bucket:Bucket::b","custom":true,"type":"aws:s3/bucket:Bucket","id":"state"}]}}`), nil
	})
	if e := d.Destroy(context.Background(), target(cfg().Projects[0], "dev")); e == nil {
		t.Fatal("backend bucket accepted")
	}
	for _, call := range f.calls {
		if strings.Contains(call, "refresh") || strings.Contains(call, "delete") || strings.Contains(call, "destroy") {
			t.Fatal(call)
		}
	}
}
func TestCloudSecretsProviderPreventsEarlyKMSDestruction(t *testing.T) {
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if a[1] == "ls" {
			return []byte(`[{"name":"dev"}]`), nil
		}
		return []byte(`{"deployment":{"secrets_providers":{"type":"cloud","state":"{\"url\":\"awskms://alias/state\"}"},"resources":[{"urn":"urn:pulumi:dev::app::aws:kms/key:Key::k","custom":true,"type":"aws:kms/key:Key","id":"key-id"}]}}`), nil
	})
	if e := d.Destroy(context.Background(), target(cfg().Projects[0], "dev")); e == nil {
		t.Fatal("cloud secrets key accepted")
	}
	for _, call := range f.calls {
		if strings.Contains(call, "refresh") || strings.Contains(call, "destroy") {
			t.Fatal(call)
		}
	}
}
func TestConfiguredRecoveryKeyIDMatchesARN(t *testing.T) {
	d, _ := newFake(t, func(string, []string) ([]byte, error) { return nil, nil })
	d.Config.Recovery = []teardown.Artifact{recovery("recovery_key", "arn:aws:kms:us-east-1:123456789012:key/key-id")}
	rs := []resource{{URN: "urn:pulumi:dev::app::aws:kms/key:Key::k", Type: "aws:kms/key:Key", ID: "key-id"}}
	if e := d.ownership(target(cfg().Projects[0], "dev"), rs); e == nil {
		t.Fatal("managed recovery key accepted")
	}
}
func TestOSRunnerOutputHelper(t *testing.T) {
	switch os.Getenv("COMPOSER_TEST_HELPER") {
	case "success":
		os.Stdout.WriteString(`{"ok":true}`)
		os.Stderr.WriteString("warning on stderr")
		os.Exit(0)
	case "failure":
		os.Stderr.WriteString("An error occurred (AccessDenied) when calling the Test operation: secret")
		os.Exit(1)
	}
}
func TestOSRunnerSeparatesSuccessfulStderr(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	b, e := (OSRunner{}).Run(context.Background(), "", []string{"COMPOSER_TEST_HELPER=success"}, exe, "-test.run=^TestOSRunnerOutputHelper$")
	if e != nil || string(b) != `{"ok":true}` {
		t.Fatal(string(b), e)
	}
	b, e = (OSRunner{}).Run(context.Background(), "", []string{"COMPOSER_TEST_HELPER=failure"}, exe, "-test.run=^TestOSRunnerOutputHelper$")
	if e == nil || !strings.Contains(cliError("operation", b).Error(), "AccessDenied") || strings.Contains(cliError("operation", b).Error(), "secret") {
		t.Fatal(string(b), e)
	}
}

func TestComponentsAreSkippedButUnknownCustomResourcesRemainUnverified(t *testing.T) {
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		if a[1] == "ls" {
			return []byte(`[{"name":"dev"}]`), nil
		}
		return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::example:index:Component::c","type":"example:index:Component","custom":false},{"urn":"urn:pulumi:dev::app::example:index:Resource::r","type":"example:index:Resource","custom":true,"id":"physical-id"}]}}`), nil
	})
	as, e := d.Inventory(context.Background(), target(cfg().Projects[0], "dev"))
	if e != nil || len(as) != 1 || as[0].ID != "physical-id" || as[0].Kind != "unsupported" {
		t.Fatal(as, e)
	}
	if state, e := d.Inspect(context.Background(), as[0]); e == nil || state == teardown.StateAbsent {
		t.Fatal(state, e)
	}
}
func TestComponentsDoNotPreventEmptyStackRemoval(t *testing.T) {
	destroyed, removed := false, false
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		if a[0] == "refresh" {
			return nil, nil
		}
		if a[0] == "destroy" {
			destroyed = true
			return nil, nil
		}
		switch a[1] {
		case "ls":
			return []byte(`[{"name":"dev"}]`), nil
		case "export":
			return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::example:index:Component::c","type":"example:index:Component","custom":false}]}}`), nil
		case "rm":
			if !destroyed {
				t.Fatal("removal before destroy")
			}
			removed = true
			return nil, nil
		}
		t.Fatal(n, a)
		return nil, nil
	})
	if e := d.Destroy(context.Background(), target(cfg().Projects[0], "dev")); e != nil || !removed {
		t.Fatal(e, removed)
	}
}
func TestMissingCustomFlagDoesNotImplyComponent(t *testing.T) {
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		if a[1] == "ls" {
			return []byte(`[{"name":"dev"}]`), nil
		}
		return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::example:index:Resource::r","type":"example:index:Resource","id":"physical-id"}]}}`), nil
	})
	if _, e := d.Inventory(context.Background(), target(cfg().Projects[0], "dev")); e == nil {
		t.Fatal("incomplete custom resource accepted")
	}
}
func TestPrimaryArtifactRejectsForeignTargetID(t *testing.T) {
	d, f := newFake(t, func(n string, a []string) ([]byte, error) { t.Fatal("resource read executed", n, a); return nil, nil })
	s, e := d.Inspect(context.Background(), teardown.Artifact{ID: "fn", Kind: "lambda", TargetID: "s3://foreign|app|dev"})
	if e == nil || s == teardown.StateAbsent || len(f.calls) != 1 {
		t.Fatal(s, e, f.calls)
	}
}
func TestPendingOperationsRejectBeforeAnyMutation(t *testing.T) {
	for _, pending := range []string{`[{"type":"creating","resource":{"custom":true,"type":"aws:s3/bucket:Bucket","id":"untracked"}}]`, `null`, `{}`} {
		d, f := newFake(t, func(n string, a []string) ([]byte, error) {
			if a[1] == "ls" {
				return []byte(`[{"name":"dev"}]`), nil
			}
			return []byte(`{"deployment":{"resources":[],"pending_operations":` + pending + `}}`), nil
		})
		if e := d.Destroy(context.Background(), target(cfg().Projects[0], "dev")); e == nil {
			t.Fatal("pending operation accepted", pending)
		}
		for _, c := range f.calls {
			if strings.Contains(c, "refresh") || strings.Contains(c, "destroy") {
				t.Fatal(c)
			}
		}
	}
}
func TestPostDeleteSystemBackupCollectionBlocksRecoveryAndChecks(t *testing.T) {
	destroyed, removed := false, false
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if n == "aws" {
			switch a[0] + " " + a[1] {
			case "dynamodb list-backups":
				if !destroyed {
					return []byte(`{"BackupSummaries":[]}`), nil
				}
				return []byte(`{"BackupSummaries":[{"BackupArn":"arn:aws:dynamodb:us-east-1:123456789012:table/table/backup/postdelete","TableName":"table","BackupType":"SYSTEM","BackupStatus":"AVAILABLE"}]}`), nil
			case "dynamodb describe-table":
				if destroyed {
					return errCLI("An error occurred (ResourceNotFoundException) when calling the DescribeTable operation: gone")
				}
				return []byte(`{"Table":{}}`), nil
			case "dynamodb update-table":
				return []byte(`{}`), nil
			case "kms describe-key":
				return []byte(`{"KeyMetadata":{"KeyState":"Enabled"}}`), nil
			case "backup list-recovery-points-by-resource":
				return []byte(`{"RecoveryPoints":[]}`), nil
			}
			t.Fatal("unexpected AWS call", a)
			return nil, nil
		}
		if a[0] == "refresh" {
			return nil, nil
		}
		if a[0] == "destroy" {
			destroyed = true
			return nil, nil
		}
		switch a[1] {
		case "ls":
			if removed {
				return []byte(`[]`), nil
			}
			return []byte(`[{"name":"dev"}]`), nil
		case "export":
			if destroyed {
				return []byte(`{"deployment":{"resources":[]}}`), nil
			}
			return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::aws:dynamodb/table:Table::table","custom":true,"type":"aws:dynamodb/table:Table","id":"table"}]}}`), nil
		case "rm":
			removed = true
			return nil, nil
		}
		t.Fatal(n, a)
		return nil, nil
	})
	key := recovery("recovery_key", "key")
	d.Config.Recovery = []teardown.Artifact{key}
	engine := teardown.Engine{Scope: d.Config.Scope, Driver: d, Store: teardown.FileStore{Path: t.TempDir() + "/report.json"}, Recovery: d.Config.Recovery}
	r, e := engine.Nuke(context.Background())
	if e == nil || !removed {
		t.Fatal("expected retained postdelete backup", e, removed)
	}
	found := false
	for _, result := range r.Results {
		if result.Artifact.Kind == "table_backups" {
			found = true
			if result.Status != teardown.Remains {
				t.Fatal(result)
			}
		}
	}
	if !found {
		t.Fatal("missing persistent backup collection")
	}
	for _, c := range f.calls {
		if strings.Contains(c, "delete-backup") || strings.Contains(c, "schedule-key-deletion") {
			t.Fatal("SYSTEM backup or recovery key mutated", c)
		}
	}
	r, e = engine.Check(context.Background(), false)
	if e == nil {
		t.Fatal("full check falsely complete", r)
	}
	found = false
	for _, result := range r.Checks {
		if result.Artifact.Kind == "table_backups" {
			found = true
			if result.Status != teardown.Remains {
				t.Fatal(result)
			}
		}
	}
	if !found {
		t.Fatal("collection missing from full check")
	}
}
func TestDiscoveryContinuesIndependentProjectFailures(t *testing.T) {
	d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
		if n == "aws" {
			if a[1] != "head-bucket" {
				t.Fatal(a)
			}
			return []byte(`{}`), nil
		}
		for i, v := range a {
			if v == "--project" {
				if a[i+1] == "app" {
					return errCLI("denied SECRET")
				}
				if a[i+1] == "shared" {
					return []byte(`[{"name":"admin-index"}]`), nil
				}
			}
		}
		t.Fatal(a)
		return nil, nil
	})
	shared := cfg().Projects[0]
	shared.Name = "shared"
	shared.Shared = true
	d.Config.Projects = append(d.Config.Projects, shared)
	targets, e := d.Discover(context.Background(), d.Config.Scope)
	var incomplete *teardown.DiscoveryError
	if !errors.As(e, &incomplete) || len(targets) != 1 || targets[0].Project != "shared" || !targets[0].Shared || len(incomplete.Artifacts) != 1 || incomplete.Artifacts[0].Data["project"] != "app" || strings.Contains(e.Error(), "SECRET") {
		t.Fatal(targets, e, incomplete)
	}
}
func TestMultiRegionProviderRoutesInventoryAuxiliaryAndReadbacks(t *testing.T) {
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if n == "aws" {
			return []byte(`{"present":true}`), nil
		}
		if a[1] == "ls" {
			return []byte(`[{"name":"dev"}]`), nil
		}
		return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::pulumi:providers:aws::main","custom":true,"type":"pulumi:providers:aws","id":"p-main","inputs":{"region":"us-east-2"}},{"urn":"urn:pulumi:dev::app::pulumi:providers:aws::acm","custom":true,"type":"pulumi:providers:aws","id":"p-acm","outputs":{"region":"us-east-1"}},{"urn":"urn:pulumi:dev::app::aws:lambda/function:Function::fn","custom":true,"type":"aws:lambda/function:Function","id":"fn","provider":"urn:pulumi:dev::app::pulumi:providers:aws::main::p-main"},{"urn":"urn:pulumi:dev::app::aws:acm/certificate:Certificate::cert","custom":true,"type":"aws:acm/certificate:Certificate","id":"arn:aws:acm:us-east-1:123456789012:certificate/cert","provider":"urn:pulumi:dev::app::pulumi:providers:aws::acm::p-acm"}]}}`), nil
	})
	d.Config.Projects[0].Region = "us-east-2"
	as, e := d.Inventory(context.Background(), target(d.Config.Projects[0], "dev"))
	if e != nil || len(as) != 3 {
		t.Fatal(as, e)
	}
	for _, a := range as {
		want := "us-east-2"
		if a.Kind == "certificate" {
			want = "us-east-1"
		}
		if a.Region != want {
			t.Fatal(a, want)
		}
		if a.Kind == "lambda" || a.Kind == "certificate" {
			if _, e := d.Inspect(context.Background(), a); e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(strings.Join(f.envs[len(f.envs)-1], " "), "AWS_REGION="+want) {
				t.Fatal(a, f.envs)
			}
		}
	}
}
func TestUnresolvedAWSProviderReferenceBlocksMutation(t *testing.T) {
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if a[1] == "ls" {
			return []byte(`[{"name":"dev"}]`), nil
		}
		return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::aws:lambda/function:Function::fn","custom":true,"type":"aws:lambda/function:Function","id":"fn","provider":"urn:pulumi:dev::foreign::pulumi:providers:aws::p::id"}]}}`), nil
	})
	if e := d.Destroy(context.Background(), target(cfg().Projects[0], "dev")); e == nil {
		t.Fatal("unresolved provider accepted")
	}
	for _, c := range f.calls {
		if strings.Contains(c, "refresh") || strings.Contains(c, "destroy") {
			t.Fatal(c)
		}
	}
}

func TestAWSBackupPointBlocksRecoveryEvenWhenDynamoBackupListingEmpty(t *testing.T) {
	destroyed, removed := false, false
	d, f := newFake(t, func(n string, a []string) ([]byte, error) {
		if n == "aws" {
			switch a[0] + " " + a[1] {
			case "dynamodb list-backups":
				if !destroyed {
					return []byte(`{"BackupSummaries":[]}`), nil
				}
				return []byte(`{"BackupSummaries":[]}`), nil
			case "dynamodb describe-table":
				if destroyed {
					return errCLI("An error occurred (ResourceNotFoundException) when calling the DescribeTable operation: gone")
				}
				return []byte(`{"Table":{}}`), nil
			case "dynamodb update-table":
				return []byte(`{}`), nil
			case "kms describe-key":
				return []byte(`{"KeyMetadata":{"KeyState":"Enabled"}}`), nil
			case "backup list-recovery-points-by-resource":
				return []byte(`{"RecoveryPoints":[{"RecoveryPointArn":"arn:aws:backup:us-east-1:123456789012:recovery-point/point"}]}`), nil
			}
			t.Fatal("unexpected AWS call", a)
			return nil, nil
		}
		if a[0] == "refresh" {
			return nil, nil
		}
		if a[0] == "destroy" {
			destroyed = true
			return nil, nil
		}
		switch a[1] {
		case "ls":
			if removed {
				return []byte(`[]`), nil
			}
			return []byte(`[{"name":"dev"}]`), nil
		case "export":
			if destroyed {
				return []byte(`{"deployment":{"resources":[]}}`), nil
			}
			return []byte(`{"deployment":{"resources":[{"urn":"urn:pulumi:dev::app::aws:dynamodb/table:Table::table","custom":true,"type":"aws:dynamodb/table:Table","id":"table"}]}}`), nil
		case "rm":
			removed = true
			return nil, nil
		}
		t.Fatal(n, a)
		return nil, nil
	})
	key := recovery("recovery_key", "key")
	d.Config.Recovery = []teardown.Artifact{key}
	engine := teardown.Engine{Scope: d.Config.Scope, Driver: d, Store: teardown.FileStore{Path: t.TempDir() + "/report.json"}, Recovery: d.Config.Recovery}
	r, e := engine.Nuke(context.Background())
	if e == nil || !removed {
		t.Fatal("expected retained AWS Backup point", e, removed)
	}
	found := false
	for _, result := range r.Results {
		if result.Artifact.Kind == "aws_backup_collection" {
			found = true
			if result.Status != teardown.Remains {
				t.Fatal(result)
			}
		}
	}
	if !found {
		t.Fatal("missing persistent backup collection")
	}
	for _, c := range f.calls {
		if strings.Contains(c, "delete-backup") || strings.Contains(c, "schedule-key-deletion") {
			t.Fatal("AWS Backup point or recovery key mutated", c)
		}
	}
	r, e = engine.Check(context.Background(), false)
	if e == nil {
		t.Fatal("full check falsely complete", r)
	}
	found = false
	for _, result := range r.Checks {
		if result.Artifact.Kind == "aws_backup_collection" {
			found = true
			if result.Status != teardown.Remains {
				t.Fatal(result)
			}
		}
	}
	if !found {
		t.Fatal("collection missing from full check")
	}
}
func TestAWSBackupCollectionRequiresAuthoritativeListing(t *testing.T) {
	for _, response := range []string{`{}`, `{"RecoveryPoints":null}`, `{"RecoveryPoints":{}}`, `denied`} {
		d, _ := newFake(t, func(n string, a []string) ([]byte, error) {
			if response == "denied" {
				return errCLI("An error occurred (AccessDeniedException) when calling the ListRecoveryPointsByResource operation: SECRET")
			}
			return []byte(response), nil
		})
		state, e := d.Inspect(context.Background(), teardown.Artifact{ID: "arn:aws:dynamodb:us-east-1:123456789012:table/table", Kind: "aws_backup_collection", Region: "us-east-1"})
		if e == nil || state == teardown.StateAbsent || strings.Contains(e.Error(), "SECRET") {
			t.Fatal(response, state, e)
		}
	}
}
