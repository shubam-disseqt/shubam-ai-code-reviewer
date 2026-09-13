// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Command zreview is the z-code-reviewer CLI.
//
// See ARCHITECTURE.md at the repo root, or run `zreview docs` for the
// bundled offline docs site.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
