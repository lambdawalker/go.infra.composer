package approval

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManualApprovalAndRejection(t *testing.T) {
	for _, yes := range []bool{false, true} {
		err := (Policy{}).Confirm(context.Background(), Routine, func(ctx context.Context) (bool, error) {
			if _, timed := ctx.Deadline(); timed {
				t.Fatal("manual policy installed a timer")
			}
			return yes, nil
		})
		if yes && err != nil || !yes && !errors.Is(err, ErrRejected) {
			t.Fatal(yes, err)
		}
	}
}

func TestOnlyRoutineActionsGetTimedApproval(t *testing.T) {
	p := Policy{Mode: Timed, Timeout: time.Millisecond}
	if err := p.Confirm(context.Background(), Routine, func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, ctx.Err() }); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []Kind{Sensitive, Destructive} {
		err := p.Confirm(context.Background(), kind, func(ctx context.Context) (bool, error) {
			if _, timed := ctx.Deadline(); timed {
				t.Fatal("protected action received auto-approval deadline")
			}
			return false, nil
		})
		if !errors.Is(err, ErrRejected) {
			t.Fatal(kind, err)
		}
	}
}

func TestCancellationAndErrorsNeverBecomeApproval(t *testing.T) {
	p := Policy{Mode: Timed, Timeout: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := p.Confirm(ctx, Routine, func(context.Context) (bool, error) { called = true; return true, nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatal(err, called)
	}
	parent, stop := context.WithTimeout(context.Background(), time.Millisecond)
	defer stop()
	if err := (Policy{Mode: Timed, Timeout: time.Hour}).Confirm(parent, Routine, func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, ctx.Err() }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("parent deadline approved", err)
	}
	terminal := errors.New("terminal disconnected")
	if err := p.Confirm(context.Background(), Routine, func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, terminal }); !errors.Is(err, terminal) {
		t.Fatal("real error ignored after timeout", err)
	}
	if err := p.Confirm(context.Background(), Routine, func(ctx context.Context) (bool, error) { <-ctx.Done(); return false, errors.Join(ctx.Err(), terminal) }); !errors.Is(err, terminal) {
		t.Fatal("mixed terminal/deadline error approved", err)
	}
	if err := p.Confirm(context.Background(), Routine, func(context.Context) (bool, error) { return false, context.DeadlineExceeded }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unrelated timeout approved", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	if err := p.Confirm(ctx, Routine, func(context.Context) (bool, error) { cancel(); return true, nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("late answer overrode cancellation", err)
	}
	if err := p.Confirm(context.Background(), Routine, func(context.Context) (bool, error) { return false, nil }); !errors.Is(err, ErrRejected) {
		t.Fatal("explicit rejection ignored", err)
	}
}

func TestInvalidPolicyRejectedBeforePrompt(t *testing.T) {
	for _, p := range []Policy{{Mode: Mode(99)}, {Mode: Timed}, {Mode: Timed, Timeout: -1}} {
		if err := p.Confirm(context.Background(), Routine, func(context.Context) (bool, error) { t.Fatal("invalid policy prompted"); return true, nil }); err == nil {
			t.Fatal(p)
		}
	}
	if err := (Policy{}).Confirm(context.Background(), Kind(99), func(context.Context) (bool, error) { t.Fatal("invalid operation prompted"); return true, nil }); err == nil {
		t.Fatal("invalid kind accepted")
	}
	if err := (Policy{}).Confirm(context.Background(), Routine, nil); err == nil {
		t.Fatal("nil prompt accepted")
	}
}
