// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	dashboard "github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/metrics_dashboard"
)

// newMetricsCmd wires `sacr metrics` — a read-only HTML report over the
// session JSONL logs in ~/.sacr/sessions. Purely local; no LLM calls.
func newMetricsCmd() *cobra.Command {
	var (
		sessionsDir string
		output      string
	)
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Generate an HTML dashboard of past sacr runs",
		Long: `Read session logs under --sessions-dir and write a self-contained HTML
report summarising cost trends, findings over time, and per-run duration.

Runs entirely offline; the generated file loads Chart.js from a CDN so it
renders in any browser without a build step.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveSessionsDir(sessionsDir)
			if err != nil {
				return err
			}
			runs, err := dashboard.ParseDir(dir)
			if err != nil {
				return fmt.Errorf("parse sessions: %w", err)
			}
			f, err := os.Create(output) //nolint:gosec // output path is a user-supplied flag
			if err != nil {
				return fmt.Errorf("create %s: %w", output, err)
			}
			defer f.Close()
			if err := dashboard.Render(f, dashboard.Build(runs)); err != nil {
				return fmt.Errorf("render report: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s (%d run(s) from %s)\n", output, len(runs), dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionsDir, "sessions-dir", "", "session log directory (default ~/.sacr/sessions)")
	cmd.Flags().StringVar(&output, "output", "sacr-metrics.html", "output HTML path")
	return cmd
}

// resolveSessionsDir expands "" to ~/.sacr/sessions. Returns an error if
// the resolved path does not exist so the user isn't left with a blank
// report and no explanation.
func resolveSessionsDir(dir string) (string, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".sacr", "sessions")
	}
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("sessions dir %s: %w", dir, err)
	}
	return dir, nil
}
