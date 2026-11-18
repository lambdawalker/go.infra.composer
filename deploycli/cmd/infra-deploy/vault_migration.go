package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
)

// Re-encrypt legacy vault contents with fresh nonce and new path binding; preserve the source.
func loadProjectVault(path, legacy string) (string, error) {
	if _, e := os.Stat(path); e == nil {
		return vault(path)
	} else if !os.IsNotExist(e) {
		return "", e
	}
	b, e := os.ReadFile(legacy)
	if os.IsNotExist(e) {
		return vault(path)
	}
	if e != nil {
		return "", e
	}
	if len(b) < 20 || string(b[:4]) != "OCV1" {
		return "", errors.New("invalid legacy vault; original preserved")
	}
	pass, e := secret("Unlock previous deployment vault to migrate it into .deploy:")
	if e != nil {
		return "", e
	}
	key, e := pbkdf2.Key(sha256.New, pass, b[4:20], 600000, 32)
	if e != nil {
		return "", e
	}
	defer clear(key)
	block, e := aes.NewCipher(key)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	if len(b) < 20+g.NonceSize() {
		return "", errors.New("corrupt legacy vault")
	}
	plain, e := g.Open(nil, b[20:20+g.NonceSize()], b[20+g.NonceSize():], []byte(legacy))
	if e != nil {
		return "", errors.New("legacy vault unlock failed; original preserved")
	}
	defer clear(plain)
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	out := append(append([]byte(nil), b[:20]...), nonce...)
	out = g.Seal(out, nonce, plain, []byte(path))
	if e = atomicWrite(path, out); e != nil {
		return "", e
	}
	note("Vault migrated to .deploy; original retained for recovery")
	return string(plain), nil
}
