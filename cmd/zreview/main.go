// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Command zreview is the z-code-reviewer CLI.
//
// See ARCHITECTURE.md at the repo root, or run `zreview docs` for the
// bundled offline docs site.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// Top-level exit path: kept as a plain "[zreview] error: ..." line so
		// the format matches historical grep patterns and CI failure-parsing
		// scripts. Structured logging lives inside runReview, where a slog
		// logger threads through the pipeline stages.
		fmt.Fprintf(os.Stderr, "[zreview] error: %s\n", err)
		var bx *blockerExitError
		if errors.As(err, &bx) {
			os.Exit(bx.code)
		}
		os.Exit(1)
	}
}
