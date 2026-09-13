// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package index

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/filetype"
)

// openTestStore creates a fresh SQLite store backed by a temp file so
// concurrent tests don't share state.
func openTestStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	dsn := "sqlite:///" + filepath.Join(dir, "index.db")
	store, err := NewStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestNewStoreRejectsUnknownScheme(t *testing.T) {
	t.Parallel()
	_, err := NewStore(context.Background(), "mysql:///x")
	if err == nil {
		t.Fatal("want error for unknown scheme")
	}
}

func TestNewStorePostgresDeferred(t *testing.T) {
	t.Parallel()
	_, err := NewStore(context.Background(), "postgres://localhost/x")
	if err == nil {
		t.Fatal("postgres should be explicitly rejected in this phase")
	}
}

func TestNewStoreInMemory(t *testing.T) {
	t.Parallel()
	store, err := NewStore(context.Background(), "sqlite:///:memory:")
	if err != nil {
		t.Fatalf("in-memory open: %v", err)
	}
	defer store.Close()
	if err := store.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestSchemaIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dsn := "sqlite:///" + filepath.Join(dir, "idempotent.db")
	for i := 0; i < 3; i++ {
		s, err := NewStore(context.Background(), dsn)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		_ = s.Close()
	}
}

func makeSummary(path string) FileSummary {
	return FileSummary{
		Path:        path,
		Language:    filetype.LangGo,
		Summary:     "does " + path,
		ContentHash: "hash-" + path,
		LOC:         10,
		Symbols: []SymbolInfo{
			{Name: "Foo", Kind: "function", Signature: "func Foo()", Description: "d"},
		},
		Imports: []string{"other.go"},
		SymbolRefs: []SymbolRef{
			{SourcePath: path, SourceSymbol: "Foo", TargetPath: "other.go", TargetSymbol: "Bar"},
		},
		ExternalRefs: []ExternalRef{
			{Kind: "npm_package", Target: "react", Description: "UI"},
		},
	}
}

func TestUpsertGetRoundTrip(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	fs := makeSummary("a.go")
	if err := store.UpsertSummary(ctx, fs); err != nil {
		t.Fatalf("UpsertSummary: %v", err)
	}
	got, err := store.GetSummary(ctx, "a.go")
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if got.Path != fs.Path || got.Summary != fs.Summary || got.ContentHash != fs.ContentHash {
		t.Errorf("round trip fields mismatch: got %+v want %+v", got, fs)
	}
	if len(got.Symbols) != 1 || got.Symbols[0].Name != "Foo" {
		t.Errorf("symbols not persisted: %+v", got.Symbols)
	}
	if len(got.Imports) != 1 || got.Imports[0] != "other.go" {
		t.Errorf("imports not persisted: %+v", got.Imports)
	}
	if len(got.SymbolRefs) != 1 || got.SymbolRefs[0].TargetSymbol != "Bar" {
		t.Errorf("symbol_refs not persisted: %+v", got.SymbolRefs)
	}
	if len(got.ExternalRefs) != 1 {
		t.Errorf("external_refs not persisted: %+v", got.ExternalRefs)
	}
}

func TestGetSummaryNotFound(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	_, err := store.GetSummary(context.Background(), "missing.go")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestUpsertReplacesChildren(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	first := makeSummary("a.go")
	if err := store.UpsertSummary(ctx, first); err != nil {
		t.Fatal(err)
	}

	second := first
	second.Summary = "new summary"
	second.Symbols = []SymbolInfo{{Name: "Bar", Kind: "function"}}
	second.Imports = []string{"different.go"}
	second.SymbolRefs = nil
	second.ExternalRefs = nil
	if err := store.UpsertSummary(ctx, second); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetSummary(ctx, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != "new summary" {
		t.Errorf("summary not updated: %q", got.Summary)
	}
	if len(got.Symbols) != 1 || got.Symbols[0].Name != "Bar" {
		t.Errorf("symbols not replaced: %+v", got.Symbols)
	}
	if len(got.Imports) != 1 || got.Imports[0] != "different.go" {
		t.Errorf("imports not replaced: %+v", got.Imports)
	}
	if len(got.SymbolRefs) != 0 {
		t.Errorf("symbol_refs should be cleared: %+v", got.SymbolRefs)
	}
	if len(got.ExternalRefs) != 0 {
		t.Errorf("external_refs should be cleared: %+v", got.ExternalRefs)
	}
}

func TestUpsertDedupesDuplicateSymbols(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	fs := makeSummary("a.go")
	fs.Symbols = []SymbolInfo{
		{Name: "Foo", Kind: "function", Description: "first"},
		{Name: "Foo", Kind: "function", Description: "second"},
	}
	if err := store.UpsertSummary(ctx, fs); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSummary(ctx, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Symbols) != 1 {
		t.Errorf("expected dedup, got %d symbols", len(got.Symbols))
	}
}

func TestRemovePathsCascades(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.UpsertSummary(ctx, makeSummary("a.go")); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSummary(ctx, makeSummary("b.go")); err != nil {
		t.Fatal(err)
	}
	if err := store.RemovePaths(ctx, []string{"a.go"}); err != nil {
		t.Fatal(err)
	}
	paths, err := store.ListAllPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "b.go" {
		t.Errorf("RemovePaths left wrong set: %+v", paths)
	}
	// Symbols row for a.go should be gone (FK cascade).
	_, err = store.GetSummary(ctx, "a.go")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("removed row still readable: %v", err)
	}
}

func TestListSummariesSkipsMissing(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.UpsertSummary(ctx, makeSummary("a.go")); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListSummaries(ctx, []string{"a.go", "missing.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "a.go" {
		t.Errorf("missing paths should be skipped: %+v", got)
	}
}

func TestInboundEdgeCounts(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	// a imports c and calls c.X. b calls c.X too but does not import.
	a := makeSummary("a.go")
	a.Imports = []string{"c.go"}
	a.SymbolRefs = []SymbolRef{{SourcePath: "a.go", SourceSymbol: "Foo", TargetPath: "c.go", TargetSymbol: "X"}}
	b := makeSummary("b.go")
	b.Imports = nil
	b.SymbolRefs = []SymbolRef{{SourcePath: "b.go", SourceSymbol: "Foo", TargetPath: "c.go", TargetSymbol: "X"}}
	c := makeSummary("c.go")
	c.Imports = nil
	c.SymbolRefs = nil

	for _, s := range []FileSummary{a, b, c} {
		if err := store.UpsertSummary(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	counts, err := store.GetInboundEdgeCounts(ctx, []string{"c.go"})
	if err != nil {
		t.Fatal(err)
	}
	// 1 import from a, 2 distinct symbol_refs callers (a, b) → 3.
	if counts["c.go"] != 3 {
		t.Errorf("inbound count = %d, want 3", counts["c.go"])
	}
}

func TestBlastRadius(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	// Graph: caller.go::Root -> mid.go::Middle -> target.go::Leaf.
	// Change target.go — expect depth-1 = mid.go, depth-2 = caller.go.
	target := FileSummary{Path: "target.go", Language: filetype.LangGo, Summary: "leaf", Symbols: []SymbolInfo{{Name: "Leaf", Kind: "function"}}}
	mid := FileSummary{
		Path: "mid.go", Language: filetype.LangGo, Summary: "middle",
		Symbols:    []SymbolInfo{{Name: "Middle", Kind: "function"}},
		SymbolRefs: []SymbolRef{{SourcePath: "mid.go", SourceSymbol: "Middle", TargetPath: "target.go", TargetSymbol: "Leaf"}},
	}
	caller := FileSummary{
		Path: "caller.go", Language: filetype.LangGo, Summary: "root",
		Symbols:    []SymbolInfo{{Name: "Root", Kind: "function"}},
		SymbolRefs: []SymbolRef{{SourcePath: "caller.go", SourceSymbol: "Root", TargetPath: "mid.go", TargetSymbol: "Middle"}},
	}

	for _, s := range []FileSummary{target, mid, caller} {
		if err := store.UpsertSummary(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := store.GetBlastRadius(ctx, []string{"target.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("blast radius len = %d, want 2 (mid + caller): %+v", len(entries), entries)
	}
	if entries[0].Path != "mid.go" || entries[0].Depth != 1 {
		t.Errorf("first entry should be mid at depth 1: %+v", entries[0])
	}
	if entries[1].Path != "caller.go" || entries[1].Depth != 2 {
		t.Errorf("second entry should be caller at depth 2: %+v", entries[1])
	}
	// Affected symbols should be populated per file.
	if len(entries[0].AffectedSymbols) != 1 || entries[0].AffectedSymbols[0] != "Middle" {
		t.Errorf("mid affected symbols: %+v", entries[0].AffectedSymbols)
	}
}

func TestDirectorySummaries(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.UpsertDirectorySummary(ctx, DirectorySummary{Path: "cmd", Summary: "entry points", FileCount: 3}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListDirectorySummaries(ctx, []string{"cmd", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "cmd" || got[0].FileCount != 3 {
		t.Errorf("directory list wrong: %+v", got)
	}
}

func TestManifestUpsertAndClear(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	pkgs := []PackageManifest{
		{Name: "react", Kind: "npm", Version: "18.0.0", FilePath: "package.json"},
		{Name: "lodash", Kind: "npm", Version: "4.17.0", FilePath: "package.json", IsDev: true},
		{Name: "django", Kind: "pip", Version: "5.0", FilePath: "requirements.txt"},
	}
	if err := store.UpsertManifestPackages(ctx, pkgs); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListManifestPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Errorf("expected 3 packages, got %d: %+v", len(listed), listed)
	}

	// Clearing with live={"package.json"} drops the requirements.txt row.
	if err := store.ClearManifestPackagesForMissingFiles(ctx, []string{"package.json"}); err != nil {
		t.Fatal(err)
	}
	listed, err = store.ListManifestPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Errorf("expected 2 packages after clear, got %d: %+v", len(listed), listed)
	}

	// Empty live set with existing rows — should drop everything.
	if err := store.ClearManifestPackagesForMissingFiles(ctx, nil); err != nil {
		t.Fatal(err)
	}
	listed, err = store.ListManifestPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Errorf("expected 0 packages after empty-live clear, got %d", len(listed))
	}
}

func TestManifestUpsertReplacesFileRows(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	first := []PackageManifest{
		{Name: "react", Kind: "npm", FilePath: "package.json"},
		{Name: "lodash", Kind: "npm", FilePath: "package.json"},
	}
	if err := store.UpsertManifestPackages(ctx, first); err != nil {
		t.Fatal(err)
	}
	// Re-upsert with a different set for the same file — lodash should be gone.
	second := []PackageManifest{
		{Name: "react", Kind: "npm", FilePath: "package.json"},
		{Name: "axios", Kind: "npm", FilePath: "package.json"},
	}
	if err := store.UpsertManifestPackages(ctx, second); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListManifestPackages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, p := range listed {
		names[p.Name] = true
	}
	if names["lodash"] {
		t.Errorf("lodash should be gone after re-upsert: %+v", listed)
	}
	if !names["axios"] || !names["react"] {
		t.Errorf("expected react+axios, got %+v", listed)
	}
}

func TestConcurrentUpsertNoRace(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	const goroutines = 8
	const perGoroutine = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				path := formatPath(g, i)
				if err := store.UpsertSummary(ctx, makeSummary(path)); err != nil {
					t.Errorf("upsert %s: %v", path, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	paths, err := store.ListAllPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := goroutines * perGoroutine; len(paths) != want {
		t.Errorf("expected %d rows, got %d", want, len(paths))
	}
}

func formatPath(g, i int) string {
	return "g" + itoa(g) + "-" + itoa(i) + ".go"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [16]byte
	pos := len(digits)
	for n > 0 {
		pos--
		digits[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[pos:])
}
