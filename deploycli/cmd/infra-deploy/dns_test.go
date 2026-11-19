package main

import (
	"flag"
	"strings"
	"testing"
)

func TestDNSProviderPromptAndSavedChoice(t *testing.T) {
	spec := fixture(t, "email").spec
	spec.Inputs = []inputSpec{{
		Name: "DNSProvider", Kind: "dnsProvider", Label: "Public DNS host", GitHubVariable: "BENCH_DNS_PROVIDER",
	}}
	if err := spec.validate(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fields := spec.inputFlags(flag.NewFlagSet("interactive", flag.ContinueOnError))
	answers := []string{"unknown", "2"}
	count := 0
	if err := fields.collect(spec.InputsFile, dir, false, false, func(label, fallback, _ string) (string, error) {
		if !strings.Contains(label, "1. AWS Route 53, 2. Cloudflare") || fallback != "" {
			t.Fatalf("unexpected first-run prompt %q with fallback %q", label, fallback)
		}
		if count >= len(answers) {
			t.Fatal("wizard did not accept Cloudflare")
		}
		answer := answers[count]
		count++
		return answer, nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2 || fields[0].value != "cloudflare" || fields[0].display(fields[0].value) != "Cloudflare" {
		t.Fatalf("unexpected provider selection %q after %d prompts", fields[0].value, count)
	}
	if err := fields.save(spec.InputsFile, dir); err != nil {
		t.Fatal(err)
	}
	newFields := spec.inputFlags(flag.NewFlagSet("rerun", flag.ContinueOnError))
	noPrompt := func(_, _, _ string) (string, error) {
		t.Fatal("wizard prompted for a saved or CI provider")
		return "", nil
	}
	if err := newFields.collect(spec.InputsFile, dir, false, false, noPrompt); err != nil {
		t.Fatal(err)
	}
	if got := newFields.githubVariables()["BENCH_DNS_PROVIDER"]; got != "cloudflare" {
		t.Fatalf("saved DNS provider = %q", got)
	}

	flags := flag.NewFlagSet("ci", flag.ContinueOnError)
	ciFields := spec.inputFlags(flags)
	if err := ciFields.collect(spec.InputsFile, dir, true, false, noPrompt); err == nil {
		t.Fatal("CI accepted saved provider without an explicit argument")
	}
	if err := flags.Parse([]string{"--dnsprovider", "route53"}); err != nil {
		t.Fatal(err)
	}
	if err := ciFields.collect(spec.InputsFile, dir, true, false, noPrompt); err != nil {
		t.Fatal(err)
	}
	if got := ciFields.githubVariables()["BENCH_DNS_PROVIDER"]; got != "route53" {
		t.Fatalf("explicit DNS provider = %q", got)
	}
}
