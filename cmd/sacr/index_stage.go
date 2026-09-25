// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/index"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/logutil"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// openStore opens the index Store for repo. SACR_DB_URL overrides the
// per-repo default under ~/.sacr/index/. The index is always on.
func openStore(ctx context.Context, repo string) (index.Store, error) {
	dsn, err := index.ResolveDSN(repo)
	if err != nil {
		return nil, err
	}
	return index.NewStore(ctx, dsn)
}

// missingSummaryPaths returns the subset of `changed` for which the store has
// no summary row.
func missingSummaryPaths(ctx context.Context, store index.Store, changed []string) ([]string, error) {
	if store == nil || len(changed) == 0 {
		return nil, nil
	}
	rows, err := store.ListSummaries(ctx, changed)
	if err != nil {
		return nil, fmt.Errorf("list summaries: %w", err)
	}
	have := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		have[r.Path] = struct{}{}
	}
	var missing []string
	for _, p := range changed {
		if _, ok := have[p]; !ok {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

// indexMissing summarizes changed files (plus extra, e.g. their importers)
// the store has never seen, so this review (not the next one) gets indexed
// context. Deleted files are skipped; their rows are pruned by `sacr index`.
// Best-effort: failures log and the review continues on whatever the store
// already holds.
func indexMissing(ctx context.Context, store index.Store, client llm.LLMClient, modelName, repo string, kept []model.Diff, extra []string, logger *slog.Logger) {
	log := logutil.WithStage(logger, "index")
	live := make([]string, 0, len(kept)+len(extra))
	for _, d := range kept {
		if d.IsDeleted || d.NewPath == "" || d.NewPath == "/dev/null" {
			continue
		}
		live = append(live, d.NewPath)
	}
	live = append(live, extra...)
	missing, err := missingSummaryPaths(ctx, store, live)
	if err != nil {
		log.Warn("skipping", "err", err.Error())
		return
	}
	if len(missing) == 0 {
		return
	}
	indexer := index.NewIndexer(store, client, index.IndexerOptions{Model: modelName, Logger: log})
	n, err := indexer.IndexDiff(ctx, repo, missing)
	if err != nil {
		log.Warn("continuing with partial index", "err", err.Error())
	}
	log.Info(fmt.Sprintf("indexed %d file(s)", n), "count", n, "missing", len(missing))
}
