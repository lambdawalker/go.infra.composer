package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCredentialStorageAndFormMask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.bin")
	pass := "fixture vault passphrase"
	tokens := providerCredentials{Cloudflare: "cloudflare-fixture-token", GitHub: "github-fixture-token"}
	if got, err := readCredentials(path, pass); err != nil || got != (providerCredentials{}) {
		t.Fatalf("missing vault: %v", err)
	}
	if err := saveCredentials(path, pass, tokens); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(tokens.Cloudflare)) || bytes.Contains(raw, []byte(tokens.GitHub)) {
		t.Fatal("credential vault contains plaintext")
	}
	if _, err := readCredentials(path, "wrong passphrase"); err == nil {
		t.Fatal("wrong vault passphrase was accepted")
	}
	got, err := readCredentials(path, pass)
	if err != nil || got != tokens {
		t.Fatalf("credential round trip failed: %v", err)
	}
	for _, field := range got.fields() {
		if !field.Saved || field.Value != "" || field.Kind != "password" {
			t.Fatalf("credential field exposed a value or did not offer a saved marker: %s", field.Name)
		}
	}
	values := map[string]string{cloudflareField: "", githubField: "new-token", "region": "us-east-2"}
	got.takeFormValues(values)
	if got.Cloudflare != tokens.Cloudflare || got.GitHub != "new-token" || len(values) != 1 {
		t.Fatal("blank credential did not preserve the old value or credentials remained in ordinary form values")
	}
}

func TestProviderCredentialsAreScopedToTheirCommands(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	ctx := context.WithValue(context.Background(), credentialsKey{}, providerCredentials{Cloudflare: "cloudflare-fixture", GitHub: "github-fixture"})
	for _, tc := range []struct{ tool, want, excluded string }{
		{"pulumi", "CLOUDFLARE_API_TOKEN=cloudflare-fixture", "GH_TOKEN=github-fixture"},
		{"gh", "GH_TOKEN=github-fixture", "CLOUDFLARE_API_TOKEN=cloudflare-fixture"},
		{"aws", "", "CLOUDFLARE_API_TOKEN=cloudflare-fixture"},
		{"aws", "", "GH_TOKEN=github-fixture"},
	} {
		env := toolCommand(ctx, tc.tool, "version").Environ()
		if tc.want != "" && !slices.Contains(env, tc.want) {
			t.Fatalf("%s command did not receive its credential", tc.tool)
		}
		if slices.Contains(env, tc.excluded) {
			t.Fatalf("%s command received an unrelated credential", tc.tool)
		}
	}
}
