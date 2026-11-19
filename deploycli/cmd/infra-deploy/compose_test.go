package main

import (
	"errors"
	"flag"
	"maps"
	"slices"
	"strings"
	"testing"
)

func composedFixture(t *testing.T) manifest {
	t.Helper()
	m := fixture(t, "email").spec
	m.Inputs[0].Compose = &composeSpec{
		Function:     "emailOnDomain",
		DefaultInput: "verify",
		Parameters: map[string]parameterBinding{
			"Address": {Source: "userInput"},
			"Domain":  {Source: "field", Name: "Domain"},
		},
	}
	m.Functions = map[string]functionSpec{
		"prefixedDomain": {
			Parameters: map[string]string{"Domain": "domain", "Prefix": "text"},
			Result:     "domain",
			Template:   "{{lower .Prefix}}.{{.Domain}}",
		},
	}
	m.Inputs = append(m.Inputs, inputSpec{
		Name: "Host", Label: "Delivery host", Kind: "domain", GitHubVariable: "BENCH_HOST_PREFIX",
		Compose: &composeSpec{
			Function: "prefixedDomain", DefaultInput: "Demo",
			Parameters: map[string]parameterBinding{
				"Domain": {Source: "field", Name: "Domain"},
				"Prefix": {Source: "userInput"},
			},
		},
	})
	return m
}

func TestComposedInputsResolveAndPreserveRawValues(t *testing.T) {
	m := composedFixture(t)
	if err := m.validate(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fields := m.inputFlags(fs)
	var prompts []string
	ask := func(label, fallback, display string) (string, error) {
		prompts = append(prompts, label)
		switch label {
		case m.Inputs[0].Label:
			if display != "verify@example.test" || fallback != "verify" {
				t.Fatalf("sender fallback=%q display=%q", fallback, display)
			}
			if len(prompts) == 3 {
				return "wrong@yahoo.com", nil
			}
			return "email-verify", nil
		case m.Inputs[1].Label:
			return "recipient@external.test", nil
		case m.Inputs[2].Label:
			return "example.test", nil
		default:
			if display != "demo.example.test" || fallback != "Demo" {
				t.Fatalf("custom fallback=%q display=%q", fallback, display)
			}
			return fallback, nil
		}
	}
	if err := fields.collect(t.Context(), m.InputsFile, dir, false, false, ask); err != nil {
		t.Fatal(err)
	}
	if want := []string{m.Inputs[1].Label, m.Inputs[2].Label, m.Inputs[0].Label, m.Inputs[0].Label, m.Inputs[3].Label}; !slices.Equal(prompts, want) {
		t.Fatalf("prompts=%v want %v", prompts, want)
	}
	if fields[2].value != "email-verify@example.test" || fields[3].value != "demo.example.test" {
		t.Fatalf("resolved sender/host %q and %q", fields[2].value, fields[3].value)
	}
	if err := fields.save(m.InputsFile, dir); err != nil {
		t.Fatal(err)
	}
	want := fields.githubVariables()
	if want["BENCH_SENDER"] != "email-verify" || want["BENCH_HOST_PREFIX"] != "Demo" {
		t.Fatalf("GitHub vars must use raw inputs: %v", want)
	}
	again := m.inputFlags(flag.NewFlagSet("second", flag.ContinueOnError))
	if err := again.collect(t.Context(), m.InputsFile, dir, false, false, func(_, _, _ string) (string, error) {
		return "", errors.New("saved inputs should be reused")
	}); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(want, again.githubVariables()) || again[3].value != fields[3].value {
		t.Fatal("reopening settings composed the saved output again")
	}
}

func TestCompositionConfigurationErrors(t *testing.T) {
	for _, tc := range []struct {
		description string
		change      func(*manifest)
		want        string
	}{
		{"unknown function", func(m *manifest) { m.Inputs[0].Compose.Function = "unknown" }, "unknown function"},
		{"unknown dependency", func(m *manifest) {
			m.Inputs[0].Compose.Parameters["Domain"] = parameterBinding{Source: "field", Name: "Missing"}
		}, "unknown field"},
		{"repeated dependency", func(m *manifest) {
			m.Inputs[0].Compose.Parameters["Address"] = parameterBinding{Source: "field", Name: "Domain"}
		}, "repeated dependency"},
		{"cycle", func(m *manifest) {
			m.Inputs[2].Compose = &composeSpec{
				Function: "prefixedDomain",
				Parameters: map[string]parameterBinding{
					"Prefix": {Source: "userInput"},
					"Domain": {Source: "field", Name: "Host"},
				},
			}
		}, "cyclic input dependencies"},
		{"built-in collision", func(m *manifest) { m.Functions["emailOnDomain"] = functionSpec{} }, "is a built-in"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			m := composedFixture(t)
			tc.change(&m)
			if err := m.validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %q", err, tc.want)
			}
		})
	}
}

func TestCtrlQQuitsPrompt(t *testing.T) {
	events := make(chan inputEvent, 1)
	events <- inputEvent{data: []byte{17}}
	_, err := askLoop(t.Context(), events, nil, "default", false, func(int, string) {})
	if !errors.Is(err, errQuit) {
		t.Fatalf("Ctrl+Q returned %v", err)
	}
}
