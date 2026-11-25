package wizardweb

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

func TestTeardownRequiresAllThreeSteps(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSession("test", func() {})
		ctx, cancel := s.startOperation(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- s.ConfirmTeardown(ctx, TeardownTarget{Name: "demo", Type: "test-dev"})
		}()
		answer := func(id uint64, value string) bool { return s.answer(id, reply{Value: &value}) }
		var previous uint64
		for step, expected := range []string{"yes", "yes", "test-dev"} {
			synctest.Wait()
			prompt := s.snapshot().Prompt
			if prompt == nil || prompt.Kind != "teardown" || prompt.Teardown.Step != step+1 ||
				prompt.CountdownSeconds != 0 || prompt.ID == previous {
				t.Fatalf("unexpected review step %d: %+v", step+1, prompt)
			}
			invalid := "test-dev"
			if step == 2 {
				invalid = "test-dev "
			}
			if answer(prompt.ID, invalid) || answer(previous, expected) {
				t.Fatal("accepted a wrong answer or stale prompt")
			}
			select {
			case <-done:
				t.Fatal("teardown was approved before the final answer")
			default:
			}
			if !answer(prompt.ID, expected) {
				t.Fatal("correct response rejected")
			}
			previous = prompt.ID
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestCancelTeardownFromFinalStep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSession("test", func() {})
		ctx, cancel := s.startOperation(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- s.ConfirmTeardown(ctx, TeardownTarget{Name: "demo", Type: "demo"})
		}()
		yes := "yes"
		for range 2 {
			synctest.Wait()
			if !s.answer(s.snapshot().Prompt.ID, reply{Value: &yes}) {
				t.Fatal("review response rejected")
			}
		}
		synctest.Wait()
		v := s.snapshot()
		if !s.stop(v.Run, false) {
			t.Fatal("could not cancel final review")
		}
		exact := "demo"
		if s.answer(v.Prompt.ID, reply{Value: &exact}) {
			t.Fatal("cancelled prompt still accepts replies")
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled teardown returned %v", err)
		}
	})
}
