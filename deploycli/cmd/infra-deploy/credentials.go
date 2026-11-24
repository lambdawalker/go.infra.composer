package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/lambdawalker/go.infra.composer/deploycli/internal/wizardweb"
)

const (
	cloudflareField = "credential.cloudflare"
	githubField     = "credential.github"
	credentialMagic = "ICV1"
	vaultHeaderSize = 20
)

type providerCredentials struct {
	Cloudflare string `json:"cloudflare,omitempty"`
	GitHub     string `json:"github,omitempty"`
}

type credentialsKey struct{}

func credentialsFrom(ctx context.Context) providerCredentials {
	credentials, _ := ctx.Value(credentialsKey{}).(providerCredentials)
	return credentials
}

func (c providerCredentials) fields() []wizardweb.Field {
	return []wizardweb.Field{
		{
			Name: cloudflareField, Group: "Credentials", Label: "Cloudflare API token", Kind: "password",
			Saved: c.configured(cloudflareField),
			Help:  "Used for Cloudflare DNS. Leave blank to keep the existing credential.",
		},
		{
			Name: githubField, Group: "Credentials", Label: "GitHub token", Kind: "password",
			Saved: c.configured(githubField),
			Help:  "Used for GitHub deployment setup. Leave blank to keep the existing credential or sign in with GitHub CLI.",
		},
	}
}

func (c providerCredentials) configured(field string) bool {
	switch field {
	case cloudflareField:
		return c.Cloudflare != "" || os.Getenv("CLOUDFLARE_API_TOKEN") != ""
	case githubField:
		return c.GitHub != "" || os.Getenv("GH_TOKEN") != "" || os.Getenv("GITHUB_TOKEN") != ""
	}
	return false
}

func (c *providerCredentials) takeFormValues(values map[string]string) {
	if token := strings.TrimSpace(values[cloudflareField]); token != "" {
		c.Cloudflare = token
	}
	if token := strings.TrimSpace(values[githubField]); token != "" {
		c.GitHub = token
	}
	delete(values, cloudflareField)
	delete(values, githubField)
}

func vaultCipher(pass string, salt []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, pass, salt, 600000, 32)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Keep provider credentials separate so older launchers can still unlock vault.bin.
func readCredentials(path, pass string) (providerCredentials, error) {
	var credentials providerCredentials
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return credentials, nil
	}
	if err != nil {
		return credentials, err
	}
	if len(data) < vaultHeaderSize || string(data[:4]) != credentialMagic {
		return credentials, errors.New("invalid credential vault; existing file preserved")
	}
	g, err := vaultCipher(pass, data[4:vaultHeaderSize])
	if err != nil {
		return credentials, err
	}
	end := vaultHeaderSize + g.NonceSize()
	if len(data) < end+g.Overhead() {
		return credentials, errors.New("corrupt credential vault; existing file preserved")
	}
	plain, err := g.Open(nil, data[vaultHeaderSize:end], data[end:], []byte(path))
	if err != nil {
		return credentials, errors.New("credential vault unlock failed; existing file preserved")
	}
	defer clear(plain)
	if err := json.Unmarshal(plain, &credentials); err != nil {
		return providerCredentials{}, errors.New("invalid credential vault contents; existing file preserved")
	}
	return credentials, nil
}

func saveCredentials(path, pass string, credentials providerCredentials) error {
	header := make([]byte, vaultHeaderSize)
	copy(header, credentialMagic)
	if _, err := rand.Read(header[4:]); err != nil {
		return err
	}
	g, err := vaultCipher(pass, header[4:])
	if err != nil {
		return err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	plain, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	defer clear(plain)
	return atomicWrite(path, g.Seal(append(header, nonce...), nonce, plain, []byte(path)))
}
