package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lambdawalker/go.infra.composer/reporting"
	"github.com/lambdawalker/go.infra.composer/reporting/pulumicli"
)

func TestDriftCommandUsesSavedBackendAndReadOnlyRefresh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mock executable")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
set -eu
[ "$PULUMI_BACKEND_URL" = "s3://saved-bucket/dev" ]
[ "${PULUMI_OPTION_RUN_PROGRAM+x}" != x ]
[ "$1" = refresh ] && [ "$2" = --preview-only ] && [ "$3" = --non-interactive ] && [ "$4" = --json ]
[ "$5" = --stack ] && [ "$6" = test-dev ] && [ "$7" = --event-log ]
[ "$9" = --run-program=false ] && [ "${10}" = --config-file ] && [ "${11}" = stack.yaml ]
printf '%s\n' '{"resourceOutputsEvent":{"metadata":{"urn":"changed","op":"update","new":{"id":"example"}}}}' > "$8"
`
	if err := os.WriteFile(filepath.Join(dir, "pulumi"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PULUMI_OPTION_RUN_PROGRAM", "true")
	t.Setenv("PULUMI_BACKEND_URL", "s3://other-bucket/other")
	artifacts := []reporting.Artifact{{Key: "changed", ProviderID: "example"}, {Key: "unverified", ProviderID: "example2"}}
	got, err := pulumicli.CollectRefresh(t.Context(), driftCommands{backend: "s3://saved-bucket/dev", config: "stack.yaml"},
		pulumicli.RefreshOptions{Dir: dir, Stack: "test-dev"}, artifacts)
	if err != nil || got[0].Observation.Reason != reporting.ExternalChange || got[1].Observation.Status != reporting.Unknown {
		t.Fatalf("got %#v, %v", got, err)
	}
}
