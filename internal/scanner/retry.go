// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"context"
	"time"
)

// Scanner adapters shell out to local binaries (gitleaks, semgrep,
// govulncheck). Local subprocess flakiness (transient ENOMEM, timing races
// with an editor writing files, semgrep's rule fetch hiccuping) is rare but
// real. Light retry recovers cheaply without changing the pipeline's
// best-effort semantics: a persistently broken tool still lands in the
// "log + skip" branch of runner.Run.
const (
	execRetryAttempts = 2
	execRetryDelay    = 200 * time.Millisecond
)

// execAttempt runs fn twice with a short pause between attempts on transient
// error. Returns the last error if both attempts fail. Never retries on
// ctx.Err() — a cancelled review must exit promptly.
//
// The "not installed" skip path in each adapter runs LookPath before this
// helper is reached, so a missing binary is never surfaced here.
func execAttempt(ctx context.Context, fn func() error) error {
	var last error
	for attempt := 1; attempt <= execRetryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		last = fn()
		if last == nil {
			return nil
		}
		// If the ctx tripped mid-attempt, don't retry — the whole review is
		// shutting down.
		if ctx.Err() != nil {
			return last
		}
		if attempt < execRetryAttempts {
			// A tight pause is fine — scanner adapters are local, so the
			// retry cost is dominated by re-launching the binary.
			select {
			case <-ctx.Done():
				return last
			case <-time.After(execRetryDelay):
			}
		}
	}
	return last
}
