package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSuccessfulScenarios(t *testing.T) {
	for _, scenario := range []string{"integrated", "auth", "email"} {
		t.Run(scenario, func(t *testing.T) {
			var out, err bytes.Buffer
			if code := run([]string{"-scenario", scenario, "-json"}, &out, &err); code != 0 {
				t.Fatalf("exit %d: %s", code, err.String())
			}
			var payload map[string]any
			if e := json.Unmarshal(out.Bytes(), &payload); e != nil {
				t.Fatal(e)
			}
			if payload["mode"] != "LOCAL SIMULATION" || payload["plan"] == nil || payload["result"] == nil {
				t.Fatalf("unexpected output: %s", out.String())
			}
		})
	}
}

func TestInvalidScenarios(t *testing.T) {
	for _, scenario := range []string{"self", "mutual", "long", "missing", "ambiguous"} {
		t.Run(scenario, func(t *testing.T) {
			var out, err bytes.Buffer
			if code := run([]string{"-scenario", scenario, "-json"}, &out, &err); code != 1 {
				t.Fatalf("exit %d; stdout %s stderr %s", code, out.String(), err.String())
			}
			var payload map[string]any
			if e := json.Unmarshal(err.Bytes(), &payload); e != nil {
				t.Fatal(e)
			}
			if payload["error"] == nil || out.Len() != 0 {
				t.Fatalf("unexpected output: %s %s", out.String(), err.String())
			}
		})
	}
}

func TestHumanOutputAndPlanOnly(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"-plan-only"}, &out, &err); code != 0 {
		t.Fatalf("exit %d: %s", code, err.String())
	}
	for _, want := range []string{"LOCAL SIMULATION", "Bindings", "Resource order", "automatic"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Simulated operations:") {
		t.Fatal("plan-only executed")
	}
}

func TestSyntaxErrors(t *testing.T) {
	for _, args := range [][]string{{"-scenario", "unknown"}, {"-bad"}, {"extra"}, {"-production=maybe"}} {
		var out, err bytes.Buffer
		if code := run(args, &out, &err); code != 2 {
			t.Fatalf("%v: exit %d", args, code)
		}
		if err.Len() == 0 {
			t.Fatal("missing diagnostic")
		}
	}
}

func TestProductionRejectsStandaloneTestDefaults(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"-scenario", "auth", "-environment", "production", "-json"}, &out, &err); code != 1 {
		t.Fatalf("exit %d stdout %s stderr %s", code, out.String(), err.String())
	}
}
