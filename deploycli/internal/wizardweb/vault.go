package wizardweb

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
	"time"
)

const vaultIdleTimeout = 20 * time.Minute

// vaultSession is protected by the Session mutex.
type vaultSession struct {
	passphrase string
	salt       [16]byte
	verifier   []byte
	deadline   time.Time
	timer      *time.Timer
	unlocked   chan struct{}
}

func vaultVerifier(passphrase string, salt [16]byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt[:], 600000, sha256.Size)
}

// VaultPassphrase reuses the current unlock or asks for a passphrase once.
func (s *Session) VaultPassphrase(ctx context.Context) (string, error) {
	for {
		s.mu.Lock()
		s.expireVault()
		passphrase, unlocked := s.vault.passphrase, s.vault.unlocked
		s.mu.Unlock()
		if passphrase != "" {
			return passphrase, ctx.Err()
		}
		if unlocked == nil {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-unlocked:
		}
	}
	passphrase, err := s.Ask(ctx, Prompt{Kind: "secret", Label: "Local vault passphrase"})
	if err != nil {
		return "", err
	}
	passphrase = strings.TrimSpace(passphrase)
	if passphrase == "" {
		return "", errors.New("vault passphrase required")
	}
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	verifier, err := vaultVerifier(passphrase, salt)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		clear(verifier)
		return "", err
	}
	s.vault.salt, s.vault.verifier, s.vault.passphrase = salt, verifier, passphrase
	s.protect(passphrase)
	s.touchVault()
	return passphrase, nil
}

// touchVault starts or extends the deadline. The caller holds the mutex.
func (s *Session) touchVault() {
	s.vault.deadline = time.Now().Add(vaultIdleTimeout)
	if s.vault.timer == nil {
		s.vault.timer = time.AfterFunc(vaultIdleTimeout, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.vault.passphrase == "" {
				return
			}
			if remaining := time.Until(s.vault.deadline); remaining > 0 {
				s.vault.timer.Reset(remaining)
				return
			}
			s.expireVault()
		})
	} else {
		s.vault.timer.Reset(vaultIdleTimeout)
	}
}

// expireVault also checks the deadline on interactive requests in case the timer is delayed.
func (s *Session) expireVault() {
	if s.vault.passphrase != "" && !time.Now().Before(s.vault.deadline) {
		s.vault.passphrase = ""
		s.vault.unlocked = make(chan struct{})
		s.view.Revision++
	}
}

func (s *Session) vaultLocked() bool {
	s.expireVault()
	return s.vault.unlocked != nil
}

func (s *Session) recordActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.vaultLocked() && s.vault.passphrase != "" {
		// The timer will pick up the updated deadline when it wakes.
		s.vault.deadline = time.Now().Add(vaultIdleTimeout)
	}
}

func (s *Session) unlockVault(passphrase string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.vaultLocked() {
		return false
	}
	passphrase = strings.TrimSpace(passphrase)
	if passphrase == "" {
		return false
	}
	verifier, err := vaultVerifier(passphrase, s.vault.salt)
	if err != nil {
		return false
	}
	defer clear(verifier)
	if subtle.ConstantTimeCompare(verifier, s.vault.verifier) != 1 {
		return false
	}
	s.vault.passphrase = passphrase
	close(s.vault.unlocked)
	s.vault.unlocked = nil
	s.view.Revision++
	s.touchVault()
	return true
}

// ForgetVault removes cached material when a vault cannot be opened or the session ends.
func (s *Session) ForgetVault() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vault.timer != nil {
		s.vault.timer.Stop()
	}
	if s.vault.unlocked != nil {
		close(s.vault.unlocked)
		s.view.Revision++
	}
	clear(s.vault.verifier)
	s.vault = vaultSession{}
}
