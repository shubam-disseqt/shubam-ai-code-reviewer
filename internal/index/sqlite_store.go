// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/store.py under Apache License 2.0.

package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/filetype"

	_ "modernc.org/sqlite"
)

// sqliteStore is a SQLite-backed Store. Autocommit — no long-lived
// transactions — matches Mira: partial-crash resume via content-hash
// lookup is the intended behavior. The mu guards writes so concurrent
// UpsertSummary calls serialize on the same connection (SQLite's own
// locking would raise SQLITE_BUSY otherwise under the driver's default
// pool of >1 conn).
type sqliteStore struct {
	db *sql.DB
	mu sync.Mutex
}

// newSQLiteStore opens (or creates) a SQLite database at path and applies
// the schema. Pass ":memory:" for an in-memory store.
func newSQLiteStore(ctx context.Context, path string) (*sqliteStore, error) {
	dsn := path
	if path != ":memory:" {
		// WAL + shared cache would need &cache=shared; we don't set it —
		// the mu above serializes writes and reads through the same *sql.DB
		// which handles connection pooling.
		dsn = path
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("index: open sqlite %q: %w", path, err)
	}
	// Serialize connections. modernc.org/sqlite is safe for concurrent use
	// but WAL + one writer keeps the semantics simple.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	// Recommended pragmas — WAL for concurrent readers, FK cascade so
	// removing a file drops symbols/imports/refs.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("index: pragma %q: %w", p, err)
		}
	}

	// Apply schema. Split on ';' because ExecContext of the whole script
	// works with modernc.org/sqlite but errors on some drivers — safer.
	for _, stmt := range splitStatements(sqliteSchema) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("index: apply schema: %w\nstmt: %s", err, stmt)
		}
	}

	return &sqliteStore{db: db}, nil
}

// splitStatements breaks a schema script on ';' boundaries, dropping empty
// segments and comment-only lines. Simple textual splitter — the schema in
// this file has no string literals containing ';'.
func splitStatements(script string) []string {
	var out []string
	for _, s := range strings.Split(script, ";") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (s *sqliteStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *sqliteStore) Close() error                   { return s.db.Close() }

// unixEpoch converts a REAL column value to time.Time. Preserves the zero
// value: 0.0 → time.Time{}, so downstream code can distinguish "never
// written" from "written at 1970".
func unixEpoch(f float64) time.Time {
	if f == 0 {
		return time.Time{}
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)
	return time.Unix(sec, nsec)
}

func nowEpoch() float64 { return float64(time.Now().UnixNano()) / 1e9 }

// GetSummary returns the row for path plus its symbols/imports/refs.
func (s *sqliteStore) GetSummary(ctx context.Context, path string) (FileSummary, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT path, language, summary, content_hash, loc, updated_at
		 FROM files WHERE path = ?`, path)
	var fs FileSummary
	var lang string
	var updated float64
	if err := row.Scan(&fs.Path, &lang, &fs.Summary, &fs.ContentHash, &fs.LOC, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FileSummary{}, ErrNotFound
		}
		return FileSummary{}, fmt.Errorf("index: get summary %q: %w", path, err)
	}
	fs.Language = filetype.Language(lang)
	fs.UpdatedAt = unixEpoch(updated)

	syms, err := s.loadSymbols(ctx, path)
	if err != nil {
		return FileSummary{}, err
	}
	fs.Symbols = syms

	imps, err := s.loadImports(ctx, path)
	if err != nil {
		return FileSummary{}, err
	}
	fs.Imports = imps

	refs, err := s.loadSymbolRefs(ctx, path)
	if err != nil {
		return FileSummary{}, err
	}
	fs.SymbolRefs = refs

	xrefs, err := s.loadExternalRefs(ctx, path)
	if err != nil {
		return FileSummary{}, err
	}
	fs.ExternalRefs = xrefs
	return fs, nil
}

func (s *sqliteStore) loadSymbols(ctx context.Context, path string) ([]SymbolInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, kind, signature, description FROM symbols WHERE file_path = ? ORDER BY name`, path)
	if err != nil {
		return nil, fmt.Errorf("index: load symbols %q: %w", path, err)
	}
	defer rows.Close()
	var out []SymbolInfo
	for rows.Next() {
		var sym SymbolInfo
		if err := rows.Scan(&sym.Name, &sym.Kind, &sym.Signature, &sym.Description); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}

func (s *sqliteStore) loadImports(ctx context.Context, path string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT target_path FROM imports WHERE source_path = ? ORDER BY target_path`, path)
	if err != nil {
		return nil, fmt.Errorf("index: load imports %q: %w", path, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *sqliteStore) loadSymbolRefs(ctx context.Context, path string) ([]SymbolRef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source_symbol, target_path, target_symbol FROM symbol_refs
		 WHERE source_path = ? ORDER BY source_symbol, target_path`, path)
	if err != nil {
		return nil, fmt.Errorf("index: load symbol refs %q: %w", path, err)
	}
	defer rows.Close()
	var out []SymbolRef
	for rows.Next() {
		ref := SymbolRef{SourcePath: path}
		if err := rows.Scan(&ref.SourceSymbol, &ref.TargetPath, &ref.TargetSymbol); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (s *sqliteStore) loadExternalRefs(ctx context.Context, path string) ([]ExternalRef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT kind, target, description FROM external_refs WHERE file_path = ? ORDER BY kind, target`, path)
	if err != nil {
		return nil, fmt.Errorf("index: load external refs %q: %w", path, err)
	}
	defer rows.Close()
	var out []ExternalRef
	for rows.Next() {
		var xr ExternalRef
		if err := rows.Scan(&xr.Kind, &xr.Target, &xr.Description); err != nil {
			return nil, err
		}
		out = append(out, xr)
	}
	return out, rows.Err()
}

// ListSummaries returns FileSummary rows for the given paths. Missing paths
// are silently omitted; an empty input returns an empty slice.
func (s *sqliteStore) ListSummaries(ctx context.Context, paths []string) ([]FileSummary, error) {
	out := make([]FileSummary, 0, len(paths))
	for _, p := range paths {
		fs, err := s.GetSummary(ctx, p)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, fs)
	}
	return out, nil
}

// ListAllPaths returns every file path currently in the index.
func (s *sqliteStore) ListAllPaths(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM files ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("index: list all paths: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetInboundEdgeCounts sums import-dependents + distinct symbol_ref callers
// per path. Files with more callers should get context priority.
func (s *sqliteStore) GetInboundEdgeCounts(ctx context.Context, paths []string) (map[string]int, error) {
	counts := make(map[string]int, len(paths))
	for _, p := range paths {
		var importCount, refCount int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM imports WHERE target_path = ?`, p).Scan(&importCount); err != nil {
			return nil, fmt.Errorf("index: import count %q: %w", p, err)
		}
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(DISTINCT source_path) FROM symbol_refs WHERE target_path = ?`, p).Scan(&refCount); err != nil {
			return nil, fmt.Errorf("index: symbol ref count %q: %w", p, err)
		}
		counts[p] = importCount + refCount
	}
	return counts, nil
}

// GetBlastRadius walks the call graph outward from changedPaths. Depth 1
// = direct callers; depth 2 = callers-of-callers. Sorted by (depth, path)
// so the closer impact comes first.
func (s *sqliteStore) GetBlastRadius(ctx context.Context, changedPaths []string) ([]BlastRadiusEntry, error) {
	changed := make(map[string]struct{}, len(changedPaths))
	for _, p := range changedPaths {
		changed[p] = struct{}{}
	}
	entries := map[string]*BlastRadiusEntry{}

	// Depth 1: direct callers of every symbol in the changed files.
	for _, p := range changedPaths {
		syms, err := s.loadSymbols(ctx, p)
		if err != nil {
			return nil, err
		}
		for _, sym := range syms {
			callers, err := s.callersOf(ctx, p, sym.Name)
			if err != nil {
				return nil, err
			}
			for _, c := range callers {
				if _, ok := changed[c.path]; ok {
					continue
				}
				addBlastCaller(ctx, s, entries, c.path, c.symbol, 1)
			}
		}
	}

	// Depth 2: callers of the depth-1 files' affected symbols.
	depth1 := make([]string, 0, len(entries))
	for p := range entries {
		depth1 = append(depth1, p)
	}
	depth1Set := make(map[string]struct{}, len(depth1))
	for _, p := range depth1 {
		depth1Set[p] = struct{}{}
	}
	for _, d1Path := range depth1 {
		// Snapshot the symbols list first — we mutate entries below.
		symsCopy := append([]string(nil), entries[d1Path].AffectedSymbols...)
		for _, sym := range symsCopy {
			callers, err := s.callersOf(ctx, d1Path, sym)
			if err != nil {
				return nil, err
			}
			for _, c := range callers {
				if _, ok := changed[c.path]; ok {
					continue
				}
				if _, ok := depth1Set[c.path]; ok {
					continue
				}
				addBlastCaller(ctx, s, entries, c.path, c.symbol, 2)
			}
		}
	}

	out := make([]BlastRadiusEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, *e)
	}
	// Sort by (depth, path).
	sortBlastEntries(out)
	return out, nil
}

type callGraphEdge struct {
	path   string
	symbol string
}

func (s *sqliteStore) callersOf(ctx context.Context, targetPath, targetSymbol string) ([]callGraphEdge, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source_path, source_symbol FROM symbol_refs
		 WHERE target_path = ? AND target_symbol = ?`, targetPath, targetSymbol)
	if err != nil {
		return nil, fmt.Errorf("index: callers of %q.%q: %w", targetPath, targetSymbol, err)
	}
	defer rows.Close()
	var out []callGraphEdge
	for rows.Next() {
		var e callGraphEdge
		if err := rows.Scan(&e.path, &e.symbol); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// addBlastCaller merges one (callerPath, callerSymbol) pair into entries at
// the given depth. If callerPath is new, its summary is fetched.
func addBlastCaller(ctx context.Context, s *sqliteStore, entries map[string]*BlastRadiusEntry, callerPath, callerSymbol string, depth int) {
	entry, ok := entries[callerPath]
	if !ok {
		var summary string
		_ = s.db.QueryRowContext(ctx,
			`SELECT summary FROM files WHERE path = ?`, callerPath).Scan(&summary)
		entry = &BlastRadiusEntry{Path: callerPath, Summary: summary, Depth: depth}
		entries[callerPath] = entry
	}
	for _, existing := range entry.AffectedSymbols {
		if existing == callerSymbol {
			return
		}
	}
	entry.AffectedSymbols = append(entry.AffectedSymbols, callerSymbol)
}

func sortBlastEntries(entries []BlastRadiusEntry) {
	// Simple insertion sort — blast radius is small (dozens at most).
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && blastLess(entries[j], entries[j-1]); j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

func blastLess(a, b BlastRadiusEntry) bool {
	if a.Depth != b.Depth {
		return a.Depth < b.Depth
	}
	return a.Path < b.Path
}

// ListDirectorySummaries returns rows for the requested directory paths.
// Missing paths are omitted from the result.
func (s *sqliteStore) ListDirectorySummaries(ctx context.Context, dirs []string) ([]DirectorySummary, error) {
	out := make([]DirectorySummary, 0, len(dirs))
	for _, d := range dirs {
		var ds DirectorySummary
		var updated float64
		err := s.db.QueryRowContext(ctx,
			`SELECT path, summary, file_count, updated_at FROM directories WHERE path = ?`, d).
			Scan(&ds.Path, &ds.Summary, &ds.FileCount, &updated)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("index: dir summary %q: %w", d, err)
		}
		ds.UpdatedAt = unixEpoch(updated)
		out = append(out, ds)
	}
	return out, nil
}

// ListManifestPackages returns every declared package, sorted by (name,
// file_path). Case-insensitive name ordering matches Mira.
func (s *sqliteStore) ListManifestPackages(ctx context.Context) ([]PackageManifest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, kind, version, file_path, is_dev, updated_at
		 FROM package_manifests ORDER BY name COLLATE NOCASE, file_path`)
	if err != nil {
		return nil, fmt.Errorf("index: list manifest packages: %w", err)
	}
	defer rows.Close()
	var out []PackageManifest
	for rows.Next() {
		var pkg PackageManifest
		var isDev int
		var updated float64
		if err := rows.Scan(&pkg.Name, &pkg.Kind, &pkg.Version, &pkg.FilePath, &isDev, &updated); err != nil {
			return nil, err
		}
		pkg.IsDev = isDev != 0
		pkg.UpdatedAt = unixEpoch(updated)
		out = append(out, pkg)
	}
	return out, rows.Err()
}

// UpsertSummary inserts or replaces one file row plus its children. Autocommit
// on completion — a crash mid-run leaves earlier files intact and re-runnable
// via content-hash guard.
func (s *sqliteStore) UpsertSummary(ctx context.Context, fs FileSummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("index: begin upsert %q: %w", fs.Path, err)
	}
	defer func() { _ = tx.Rollback() }()

	now := nowEpoch()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO files (path, language, summary, content_hash, loc, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		   language=excluded.language,
		   summary=excluded.summary,
		   content_hash=excluded.content_hash,
		   loc=excluded.loc,
		   updated_at=excluded.updated_at`,
		fs.Path, string(fs.Language), fs.Summary, fs.ContentHash, fs.LOC, now); err != nil {
		return fmt.Errorf("index: upsert file %q: %w", fs.Path, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM symbols WHERE file_path = ?`, fs.Path); err != nil {
		return err
	}
	seenSym := make(map[string]struct{}, len(fs.Symbols))
	for _, sym := range fs.Symbols {
		if sym.Name == "" {
			continue
		}
		if _, dup := seenSym[sym.Name]; dup {
			continue
		}
		seenSym[sym.Name] = struct{}{}
		kind := sym.Kind
		if kind == "" {
			kind = "function"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO symbols (file_path, name, kind, signature, description)
			 VALUES (?, ?, ?, ?, ?)`,
			fs.Path, sym.Name, kind, sym.Signature, sym.Description); err != nil {
			return fmt.Errorf("index: insert symbol %q.%q: %w", fs.Path, sym.Name, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM imports WHERE source_path = ?`, fs.Path); err != nil {
		return err
	}
	seenImp := make(map[string]struct{}, len(fs.Imports))
	for _, imp := range fs.Imports {
		if imp == "" {
			continue
		}
		if _, dup := seenImp[imp]; dup {
			continue
		}
		seenImp[imp] = struct{}{}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO imports (source_path, target_path) VALUES (?, ?)`,
			fs.Path, imp); err != nil {
			return fmt.Errorf("index: insert import %q→%q: %w", fs.Path, imp, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM symbol_refs WHERE source_path = ?`, fs.Path); err != nil {
		return err
	}
	seenRef := map[[3]string]struct{}{}
	for _, ref := range fs.SymbolRefs {
		if ref.SourceSymbol == "" || ref.TargetPath == "" || ref.TargetSymbol == "" {
			continue
		}
		key := [3]string{ref.SourceSymbol, ref.TargetPath, ref.TargetSymbol}
		if _, dup := seenRef[key]; dup {
			continue
		}
		seenRef[key] = struct{}{}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO symbol_refs (source_path, source_symbol, target_path, target_symbol)
			 VALUES (?, ?, ?, ?)`,
			fs.Path, ref.SourceSymbol, ref.TargetPath, ref.TargetSymbol); err != nil {
			return fmt.Errorf("index: insert symbol_ref: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM external_refs WHERE file_path = ?`, fs.Path); err != nil {
		return err
	}
	seenXR := map[[2]string]struct{}{}
	for _, xr := range fs.ExternalRefs {
		if xr.Kind == "" || xr.Target == "" {
			continue
		}
		key := [2]string{xr.Kind, xr.Target}
		if _, dup := seenXR[key]; dup {
			continue
		}
		seenXR[key] = struct{}{}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO external_refs (file_path, kind, target, description)
			 VALUES (?, ?, ?, ?)`,
			fs.Path, xr.Kind, xr.Target, xr.Description); err != nil {
			return fmt.Errorf("index: insert external_ref: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("index: commit upsert %q: %w", fs.Path, err)
	}
	return nil
}

// UpsertDirectorySummary inserts or replaces one directory row.
func (s *sqliteStore) UpsertDirectorySummary(ctx context.Context, d DirectorySummary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO directories (path, summary, file_count, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET
		   summary=excluded.summary,
		   file_count=excluded.file_count,
		   updated_at=excluded.updated_at`,
		d.Path, d.Summary, d.FileCount, nowEpoch())
	if err != nil {
		return fmt.Errorf("index: upsert directory %q: %w", d.Path, err)
	}
	return nil
}

// UpsertManifestPackages replaces every package_manifests row for the file
// paths in pkgs. Grouping by file_path lets one call re-mirror a manifest
// after re-parse. Empty pkgs is a no-op.
func (s *sqliteStore) UpsertManifestPackages(ctx context.Context, pkgs []PackageManifest) error {
	if len(pkgs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("index: begin upsert pkgs: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	byFile := map[string][]PackageManifest{}
	order := []string{}
	for _, p := range pkgs {
		if _, ok := byFile[p.FilePath]; !ok {
			order = append(order, p.FilePath)
		}
		byFile[p.FilePath] = append(byFile[p.FilePath], p)
	}

	now := nowEpoch()
	for _, fp := range order {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM package_manifests WHERE file_path = ?`, fp); err != nil {
			return fmt.Errorf("index: clear pkgs %q: %w", fp, err)
		}
		for _, p := range byFile[fp] {
			isDev := 0
			if p.IsDev {
				isDev = 1
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT OR REPLACE INTO package_manifests
				 (name, kind, version, file_path, is_dev, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				p.Name, p.Kind, p.Version, p.FilePath, isDev, now); err != nil {
				return fmt.Errorf("index: insert pkg %q: %w", p.Name, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("index: commit upsert pkgs: %w", err)
	}
	return nil
}

// ClearManifestPackagesForMissingFiles drops rows whose file_path is not in
// live. Runs even if live is empty (a deleted manifest must still clear its
// stale rows).
func (s *sqliteStore) ClearManifestPackagesForMissingFiles(ctx context.Context, live []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT file_path FROM package_manifests`)
	if err != nil {
		return fmt.Errorf("index: list manifest files: %w", err)
	}
	var existing []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			_ = rows.Close()
			return err
		}
		existing = append(existing, p)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(existing) == 0 {
		return nil
	}

	liveSet := make(map[string]struct{}, len(live))
	for _, l := range live {
		liveSet[l] = struct{}{}
	}

	var stale []string
	for _, p := range existing {
		if _, ok := liveSet[p]; !ok {
			stale = append(stale, p)
		}
	}
	if len(stale) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range stale {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM package_manifests WHERE file_path = ?`, p); err != nil {
			return fmt.Errorf("index: delete stale pkg %q: %w", p, err)
		}
	}
	return tx.Commit()
}

// RemovePaths deletes files rows for the given paths. FK cascade drops
// symbols/imports/refs/external_refs. Empty input is a no-op.
func (s *sqliteStore) RemovePaths(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range paths {
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path = ?`, p); err != nil {
			return fmt.Errorf("index: delete %q: %w", p, err)
		}
	}
	return tx.Commit()
}
