// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/index"
)

func newIndexCmd() *cobra.Command {
	var (
		repo  string
		full  bool
		paths []string
	)
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build (or refresh) the SQLite code index",
		Long: `Walk the repository, summarize each eligible file via the configured LLM,
and persist the results in the repo's SQLite index (~/.sacr/index/<hash>.db,
or the DSN in SACR_DB_URL).

Existing rows whose content hash is unchanged are skipped unless --full is set.

Pass --paths to re-summarize only the listed files. The review command indexes
changed files it has not seen on its own; run this to pre-warm a whole repo.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIndex(cmd.Context(), cmd, repo, full, paths)
		},
	}
	cmd.Flags().StringVar(&repo, "repo", ".", "repository root to index")
	cmd.Flags().BoolVar(&full, "full", false, "re-summarize every file (ignore content-hash cache)")
	cmd.Flags().StringSliceVar(&paths, "paths", nil, "re-summarize only these repo-relative paths (comma-separated or repeated)")
	return cmd
}

func runIndex(ctx context.Context, cmd *cobra.Command, repo string, full bool, paths []string) error {
	store, err := openStore(ctx, repo)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	tiers, err := newLLMTiers()
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}

	status := &index.Status{}
	indexer := index.NewIndexer(store, tiers.Cheap, index.IndexerOptions{
		Model:  tiers.CheapModel,
		Full:   full,
		Status: status,
	})
	var count int
	if len(paths) > 0 {
		count, err = indexer.IndexDiff(ctx, repo, paths)
	} else {
		count, err = indexer.IndexRepo(ctx, repo)
	}
	if err != nil {
		return fmt.Errorf("index repo: %w", err)
	}
	snap := status.Snapshot()
	fmt.Fprintf(cmd.OutOrStdout(), "indexed %d files (total %d, elapsed %s)\n",
		count, snap.Total, snap.EndedAt.Sub(snap.StartedAt))
	return nil
}
