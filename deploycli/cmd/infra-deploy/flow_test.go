package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestActionsNeverEscalateAndApplySavedPlan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mock executables; native Windows launcher has separate CI")
	}
	mockTool, e := os.ReadFile("testdata/tools.py")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"ocr", "email"} {
		cli := fixture(t, name)
		for _, tc := range []struct {
			action                                                            string
			approve, previewFail, listFail, destructive, wantApply, wantError bool
		}{
			{action: "build"}, {action: "status"}, {action: "setup"}, {action: "preview"}, {action: "report"},
			{action: "deploy", wantError: true}, {action: "deploy", approve: true, wantApply: true},
			{action: "deploy", approve: true, previewFail: true, wantError: true}, {action: "preview", listFail: true, wantError: true},
			{action: "deploy", approve: true, destructive: true, wantError: true},
		} {
			t.Run(name+"/"+tc.action+strings.Repeat("-failure", boolInt(tc.wantError)), func(t *testing.T) {
				root := t.TempDir()
				bin := t.TempDir()
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home)
				for _, name := range []string{"aws", "pulumi", "go"} {
					if e = os.WriteFile(filepath.Join(bin, name), mockTool, 0700); e != nil {
						t.Fatal(e)
					}
				}
				os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test"), 0600)
				os.WriteFile(filepath.Join(root, "source.go"), []byte("package main"), 0600)
				os.MkdirAll(filepath.Join(root, cli.spec.PulumiDir), 0700)
				log := filepath.Join(root, "commands.jsonl")
				t.Setenv("MOCK_LOG", log)
				t.Setenv("MOCK_STACK", cli.spec.StackPrefix+"dev")
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("PULUMI_CONFIG_PASSPHRASE", "test-only")
				t.Setenv("MOCK_PREVIEW_FAIL", "")
				t.Setenv("MOCK_LIST_FAIL", "")
				t.Setenv("MOCK_DESTRUCTIVE", "")
				if tc.previewFail {
					t.Setenv("MOCK_PREVIEW_FAIL", "1")
				}
				if tc.listFail {
					t.Setenv("MOCK_LIST_FAIL", "1")
				}
				if tc.destructive {
					t.Setenv("MOCK_DESTRUCTIVE", "1")
				}
				oldDir, _ := os.Getwd()
				os.Chdir(root)
				defer os.Chdir(oldDir)
				oldArgs := os.Args
				defer func() { os.Args = oldArgs }()
				os.Args = []string{"deploy", "--non-interactive", "--project", "test", "--environment", "dev", "--account", "123456789012", "--region", "us-east-2", "--backend", "s3://state/prefix", "--action", tc.action}
				if name == "ocr" {
					os.Args = append(os.Args, "--memory", "1024")
				} else {
					os.Args = append(os.Args, "--sender", "sender@example.test", "--recipient", "recipient@example.test", "--domain", "example.test")
				}
				if tc.approve {
					os.Args = append(os.Args, "--approve")
				}
				if name == "email" && tc.action == "preview" {
					cli.spec.Discovery = &discoverySpec{
						APIURL: cli.spec.Outputs[0], ProbeMethod: "GET", ProbePath: "/health", ProbeStatus: 200,
					}
					os.Args = append(os.Args, "--discovery-index", "https://index.example.invalid/v1/environments")
				}
				e = cli.run()
				if (e != nil) != tc.wantError {
					t.Fatal(e)
				}
				b, _ := os.ReadFile(log)
				applied := strings.Contains(string(b), `["pulumi", "up",`)
				if applied != tc.wantApply {
					t.Fatal(string(b), e)
				}
				if applied && !strings.Contains(string(b), `"--plan"`) {
					t.Fatal("apply did not use saved plan")
				}
				if tc.listFail && strings.Contains(string(b), `"init"`) {
					t.Fatal("failed discovery initialized")
				}
				if tc.action == "preview" {
					path := filepath.Join(home, ".infra-composer", "test", "environments", "dev", "discovery.json")
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatal("preview changed discovery publication settings")
					}
				}
				if tc.wantError {
					if _, e = os.Stat(filepath.Join(home, ".infra-composer", "test", "environments", "dev", "applied.json")); !os.IsNotExist(e) {
						t.Fatal("failed attempt recorded apply success")
					}
				}
			})
		}
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
