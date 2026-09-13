// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/store.py under Apache License 2.0.

package index

import (
	"context"
	"fmt"
	"strings"
)

// Store is the persistence backend for the code index. All writes are
// autocommit — one file per commit — so a partial-crash indexing run can
// resume where it left off by matching content hashes.
type Store interface {
	// Reads
	GetSummary(ctx context.Context, path string) (FileSummary, error)
	ListSummaries(ctx context.Context, paths []string) ([]FileSummary, error)
	ListAllPaths(ctx context.Context) ([]string, error)
	GetInboundEdgeCounts(ctx context.Context, paths []string) (map[string]int, error)
	GetBlastRadius(ctx context.Context, changedPaths []string) ([]BlastRadiusEntry, error)
	ListDirectorySummaries(ctx context.Context, dirs []string) ([]DirectorySummary, error)
	ListManifestPackages(ctx context.Context) ([]PackageManifest, error)

	// Writes (per-file, autocommit).
	UpsertSummary(ctx context.Context, s FileSummary) error
	UpsertDirectorySummary(ctx context.Context, d DirectorySummary) error
	UpsertManifestPackages(ctx context.Context, pkgs []PackageManifest) error
	ClearManifestPackagesForMissingFiles(ctx context.Context, live []string) error
	RemovePaths(ctx context.Context, paths []string) error

	// Lifecycle
	Ping(ctx context.Context) error
	Close() error
}

// ErrNotFound is returned by GetSummary when the requested path is unknown.
var ErrNotFound = fmt.Errorf("index: summary not found")

// NewStore opens a Store for the given DSN. Supported schemes:
//
//	sqlite:///absolute/path.db
//	sqlite:///:memory:
//
// Postgres is deferred to a later phase; any other scheme returns an error.
func NewStore(ctx context.Context, dsn string) (Store, error) {
	switch {
	case strings.HasPrefix(dsn, "sqlite:///"):
		path := strings.TrimPrefix(dsn, "sqlite:///")
		return newSQLiteStore(ctx, path)
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return nil, fmt.Errorf("index: postgres backend not implemented in this phase")
	default:
		return nil, fmt.Errorf("index: unsupported DSN scheme %q (want sqlite:///path or sqlite:///:memory:)", dsn)
	}
}
