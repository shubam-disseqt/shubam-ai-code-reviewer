// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/indexer.py
// (_index_manifests) under Apache License 2.0.

package index

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/manifests"
)

// IndexManifests walks repo-relative tree paths, delegates each match to the
// manifest registry, converts declared packages into PackageManifest rows,
// and upserts them into the store. A per-file parse error is logged and
// skipped so one bad file cannot abort the whole pass.
//
// tree must be repo-relative slash-paths (as walkRepoTree emits). absRoot is
// the filesystem root the paths are read from.
func IndexManifests(ctx context.Context, store Store, absRoot string, tree []string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	reg := manifests.NewRegistry()

	now := time.Now().UTC()
	// Collect rows across every manifest file first, then upsert in one call
	// so DELETE-by-file semantics on the store side stay coherent.
	var rows []PackageManifest
	var liveManifestPaths []string

	for _, rel := range tree {
		if !reg.Match(rel) {
			continue
		}
		liveManifestPaths = append(liveManifestPaths, rel)
		abs := filepath.Join(absRoot, filepath.FromSlash(rel))
		b, err := os.ReadFile(abs)
		if err != nil {
			logger.Warn("index: read manifest", "path", rel, "err", err)
			continue
		}
		pkgs, err := reg.Parse(rel, string(b))
		if err != nil {
			logger.Warn("index: parse manifest", "path", rel, "err", err)
			continue
		}
		for _, p := range pkgs {
			if p.Name == "" {
				continue
			}
			rows = append(rows, PackageManifest{
				Name:      p.Name,
				Kind:      p.Kind,
				Version:   p.Version,
				FilePath:  p.FilePath,
				IsDev:     p.IsDev,
				UpdatedAt: now,
			})
		}
	}

	if err := store.UpsertManifestPackages(ctx, rows); err != nil {
		return fmt.Errorf("index: upsert manifest packages: %w", err)
	}
	// Purge rows for manifest files that no longer exist. ClearManifest…
	// keeps entries whose file_path IS in `live`.
	if err := store.ClearManifestPackagesForMissingFiles(ctx, liveManifestPaths); err != nil {
		logger.Warn("index: clear stale manifest packages", "err", err)
	}
	return nil
}
