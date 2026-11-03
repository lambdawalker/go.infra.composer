// Package approval provides UI-independent deployment approval policy.
package approval

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Mode uint8

const (
	// Manual is the default: the caller must explicitly approve.
	Manual Mode = iota
	Timed
)

type Kind uint8

const (
	// Routine permits an explicitly enabled timed approval.
	Routine Kind = iota
	// Sensitive and Destructive always require an explicit answer.
	Sensitive
	Destructive
)

// Policy does not render prompts or invoke deployment tools. Applications choose
// the duration; Composer deliberately has no global policy or default timeout.
type Policy struct {
	Mode    Mode
	Timeout time.Duration
}

var ErrRejected = errors.New("cancelled")
var deadlineExpired = errors.New("approval deadline expired")

// Prompt must honor its context and return ctx.Err() when interrupted by that
// context (unwrapped). Rejection is false,nil. Input failures must remain their own errors.
type Prompt func(context.Context) (bool, error)

// Confirm only auto-approves a routine operation when its own deadline expires
// and the prompt reports that deadline. Parent cancellation and unrelated errors
// never grant approval. No background prompt goroutine is created or left behind.
func (p Policy) Confirm(ctx context.Context, kind Kind, prompt Prompt) error {
	if ctx == nil || prompt == nil {
		return fmt.Errorf("approval requires a context and prompt")
	}
	if p.Mode != Manual && p.Mode != Timed {
		return fmt.Errorf("unknown approval mode")
	}
	if p.Timeout < 0 || p.Mode == Timed && p.Timeout <= 0 {
		return fmt.Errorf("timed approval requires a positive timeout")
	}
	if kind != Routine && kind != Sensitive && kind != Destructive {
		return fmt.Errorf("unknown approval kind")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	wait := ctx
	if p.Mode == Timed && kind == Routine {
		var cancel context.CancelFunc
		wait, cancel = context.WithTimeoutCause(ctx, p.Timeout, deadlineExpired)
		defer cancel()
	}
	yes, err := prompt(wait)
	// Check the parent independently: its deadline is not an approval timer.
	if parentErr := ctx.Err(); parentErr != nil {
		return parentErr
	}
	if errors.Is(context.Cause(wait), deadlineExpired) && err == context.DeadlineExceeded {
		return nil
	}
	if err != nil {
		return err
	}
	if !yes {
		return ErrRejected
	}
	return nil
}
