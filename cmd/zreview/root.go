// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import "github.com/spf13/cobra"

// Version, GitCommit and BuildDate are injected via -ldflags at build time.
// See the Makefile.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "zreview",
		Short: "AI-powered pull request reviewer",
		Long: `z-code-reviewer — AI-powered pull request reviewer.

Precise per-diff comments, repo-wide context, org-level rules,
cross-PR overlap detection. Self-hosted, Go-native, CLI-first.

The full architecture lives in ARCHITECTURE.md at the repo root, or
in the bundled offline docs site — run 'zreview docs' to browse it.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCmd())
	root.AddCommand(newDocsCmd())
	root.AddCommand(newReviewCmd(), newIndexCmd(), newOverlapCmd(),
		newRulesCmd(), newDoctorCmd(), newMetricsCmd())
	return root
}
