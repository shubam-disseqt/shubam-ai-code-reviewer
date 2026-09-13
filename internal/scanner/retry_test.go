// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExecAttempt_SucceedsFirstTry(t *testing.T) {
	calls := 0
	err := execAttempt(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("execAttempt: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestExecAttempt_RetriesTransientFailure(t *testing.T) {
	calls := 0
	err := execAttempt(context.Background(), func() error {
		calls++
		if calls == 1 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("execAttempt: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestExecAttempt_GivesUpAfterMaxAttempts(t *testing.T) {
	calls := 0
	err := execAttempt(context.Background(), func() error {
		calls++
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("want error after exhausting retries")
	}
	if calls != execRetryAttempts {
		t.Errorf("calls = %d, want %d", calls, execRetryAttempts)
	}
}

func TestExecAttempt_ContextCancellationShortCircuits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	calls := 0
	err := execAttempt(ctx, func() error {
		calls++
		return nil
	})
	if err == nil {
		t.Fatal("want ctx error")
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0 (ctx canceled before first attempt)", calls)
	}
}

func TestExecAttempt_ContextCancelDuringBackoff(t *testing.T) {
	// After the first failure, the helper sleeps execRetryDelay. Cancelling
	// mid-sleep must return promptly instead of waiting the full delay.
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	start := time.Now()
	// Cancel after a short delay so the first attempt runs, then cancel
	// during the backoff pause.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := execAttempt(ctx, func() error {
		calls++
		return errors.New("fail")
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (retry cancelled during backoff)", calls)
	}
	if elapsed >= execRetryDelay {
		t.Errorf("elapsed = %v; expected early exit before %v", elapsed, execRetryDelay)
	}
}
