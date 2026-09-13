// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/shubam-disseqt/z-code-reviewer/internal/index"
)

func newIndexCmd() *cobra.Command {
	var (
		repo string
		full bool
	)
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build (or refresh) the SQLite code index",
		Long: `Walk the repository, summarize each eligible file via the configured LLM,
and persist the results in the SQLite index named by ZREVIEW_DB_URL.

Existing rows whose content hash is unchanged are skipped unless --full is set.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIndex(cmd.Context(), cmd, repo, full)
		},
	}
	cmd.Flags().StringVar(&repo, "repo", ".", "repository root to index")
	cmd.Flags().BoolVar(&full, "full", false, "re-summarize every file (ignore content-hash cache)")
	return cmd
}

func runIndex(ctx context.Context, cmd *cobra.Command, repo string, full bool) error {
	dsn := os.Getenv("ZREVIEW_DB_URL")
	if dsn == "" {
		return fmt.Errorf("ZREVIEW_DB_URL is required for `zreview index`")
	}
	store, err := index.NewStore(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	llmClient, _, err := newLLMClient()
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}

	status := &index.Status{}
	indexer := index.NewIndexer(store, llmClient, index.IndexerOptions{
		Full:   full,
		Status: status,
	})
	count, err := indexer.IndexRepo(ctx, repo)
	if err != nil {
		return fmt.Errorf("index repo: %w", err)
	}
	snap := status.Snapshot()
	fmt.Fprintf(cmd.OutOrStdout(), "indexed %d files (total %d, elapsed %s)\n",
		count, snap.Total, snap.EndedAt.Sub(snap.StartedAt))
	return nil
}
