// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shubam-disseqt/z-code-reviewer/internal/rules"
)

func newRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Inspect and refresh org review-rules",
	}
	cmd.AddCommand(newRulesListCmd(), newRulesSyncCmd())
	return cmd
}

func newRulesListCmd() *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List loaded review rules with id/scope/paths/severity",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRulesList(cmd.Context(), cmd, source)
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "override the rules source (repo spec or local dir)")
	return cmd
}

func newRulesSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Force-refresh the org rules cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRulesSync(cmd.Context(), cmd)
		},
	}
}

// resolveRulesSource picks the rules spec from --source or the env var. Empty
// string means neither is set.
func resolveRulesSource(override string) string {
	if override != "" {
		return override
	}
	return os.Getenv("ZREVIEW_ORG_RULES_REPO")
}

// rulesCacheDir returns the on-disk cache path for cloned rules repos.
func rulesCacheDir() string {
	return filepath.Join(os.TempDir(), "zreview-rules-cache")
}

func runRulesList(ctx context.Context, cmd *cobra.Command, override string) error {
	source := resolveRulesSource(override)
	if source == "" {
		return fmt.Errorf("no rules source (set --source or ZREVIEW_ORG_RULES_REPO)")
	}
	loader := rules.NewLoader(rulesCacheDir())
	all, err := loader.LoadFromRepo(ctx, source)
	if err != nil {
		return fmt.Errorf("load rules: %w", err)
	}
	w := cmd.OutOrStdout()
	if len(all) == 0 {
		fmt.Fprintln(w, "No rules found.")
		return nil
	}
	for _, r := range all {
		fmt.Fprintf(w, "- %s [%s / %s]\n", r.ID, r.Scope, r.Severity)
		if r.Title != "" {
			fmt.Fprintf(w, "    title: %s\n", r.Title)
		}
		if len(r.Paths) > 0 {
			fmt.Fprintf(w, "    paths: %s\n", strings.Join(r.Paths, ", "))
		}
		if len(r.Repos) > 0 {
			fmt.Fprintf(w, "    repos: %s\n", strings.Join(r.Repos, ", "))
		}
	}
	return nil
}

// runRulesSync removes the cache and re-clones to force a fresh checkout.
func runRulesSync(ctx context.Context, cmd *cobra.Command) error {
	source := resolveRulesSource("")
	if source == "" {
		return fmt.Errorf("no rules source (set ZREVIEW_ORG_RULES_REPO)")
	}
	dir := rulesCacheDir()
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear rules cache: %w", err)
	}
	loader := rules.NewLoader(dir)
	all, err := loader.LoadFromRepo(ctx, source)
	if err != nil {
		return fmt.Errorf("resync rules: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Synced %d rule(s) from %s\n", len(all), source)
	return nil
}
