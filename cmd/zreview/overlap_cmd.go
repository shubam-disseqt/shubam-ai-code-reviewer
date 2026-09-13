// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/shubam-disseqt/z-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/z-code-reviewer/internal/overlap"
)

func newOverlapCmd() *cobra.Command {
	var (
		owner  string
		repo   string
		number int
	)
	cmd := &cobra.Command{
		Use:   "overlap",
		Short: "Detect other open PRs that may collide with this one",
		Long: `Run the cross-PR overlap pipeline against owner/repo#number and print
the walkthrough markdown block to stdout.

Requires GITHUB_TOKEN for private repos and a configured LLM endpoint.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOverlap(cmd.Context(), cmd, owner, repo, number)
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "repository owner")
	cmd.Flags().StringVar(&repo, "repo", "", "repository name")
	cmd.Flags().IntVar(&number, "pr", 0, "PR number")
	_ = cmd.MarkFlagRequired("owner")
	_ = cmd.MarkFlagRequired("repo")
	_ = cmd.MarkFlagRequired("pr")
	return cmd
}

func runOverlap(ctx context.Context, cmd *cobra.Command, owner, repoName string, number int) error {
	ghc, err := gh.NewClient(gh.Options{Token: os.Getenv("GITHUB_TOKEN")})
	if err != nil {
		return fmt.Errorf("gh client: %w", err)
	}
	tiers, err := newLLMTiers()
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	llmc := tiers.Main

	cur := overlap.PR{Owner: owner, Repo: repoName, Number: number}
	findings, err := overlap.Detect(ctx, overlap.DefaultConfig(), cur, ghc, llmc)
	if err != nil {
		return fmt.Errorf("detect overlap: %w", err)
	}
	if md := overlap.Render(findings); md != "" {
		fmt.Fprintln(cmd.OutOrStdout(), md)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "No overlap detected.")
	}
	return nil
}
