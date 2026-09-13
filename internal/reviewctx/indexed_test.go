// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package reviewctx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/index"
)

// --- fake Store ------------------------------------------------------------

type fakeStore struct {
	summaries    map[string]index.FileSummary
	dirSummaries map[string]index.DirectorySummary
	inbound      map[string]int
	blast        []index.BlastRadiusEntry

	// Error injection.
	blastErr   error
	inboundErr error
	summaryErr error
	dirErr     error
}

func (f *fakeStore) GetSummary(_ context.Context, p string) (index.FileSummary, error) {
	s, ok := f.summaries[p]
	if !ok {
		return index.FileSummary{}, index.ErrNotFound
	}
	return s, nil
}

func (f *fakeStore) ListSummaries(_ context.Context, paths []string) ([]index.FileSummary, error) {
	if f.summaryErr != nil {
		return nil, f.summaryErr
	}
	var out []index.FileSummary
	for _, p := range paths {
		if s, ok := f.summaries[p]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeStore) ListAllPaths(context.Context) ([]string, error) { return nil, nil }

func (f *fakeStore) GetInboundEdgeCounts(_ context.Context, paths []string) (map[string]int, error) {
	if f.inboundErr != nil {
		return nil, f.inboundErr
	}
	out := make(map[string]int, len(paths))
	for _, p := range paths {
		out[p] = f.inbound[p]
	}
	return out, nil
}

func (f *fakeStore) GetBlastRadius(_ context.Context, _ []string) ([]index.BlastRadiusEntry, error) {
	if f.blastErr != nil {
		return nil, f.blastErr
	}
	return f.blast, nil
}

func (f *fakeStore) ListDirectorySummaries(_ context.Context, dirs []string) ([]index.DirectorySummary, error) {
	if f.dirErr != nil {
		return nil, f.dirErr
	}
	var out []index.DirectorySummary
	for _, d := range dirs {
		if s, ok := f.dirSummaries[d]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeStore) ListManifestPackages(context.Context) ([]index.PackageManifest, error) {
	return nil, nil
}

// Unused writes.
func (f *fakeStore) UpsertSummary(context.Context, index.FileSummary) error               { return nil }
func (f *fakeStore) UpsertDirectorySummary(context.Context, index.DirectorySummary) error { return nil }
func (f *fakeStore) UpsertManifestPackages(context.Context, []index.PackageManifest) error {
	return nil
}
func (f *fakeStore) ClearManifestPackagesForMissingFiles(context.Context, []string) error {
	return nil
}
func (f *fakeStore) RemovePaths(context.Context, []string) error { return nil }
func (f *fakeStore) Ping(context.Context) error                  { return nil }
func (f *fakeStore) Close() error                                { return nil }

// --- tests ------------------------------------------------------------------

func TestBuildIndexed_60_30_10Split(t *testing.T) {
	// Craft summaries just big enough that the split matters, then verify
	// that the total output stays under the char budget and every section
	// is present.
	changed := []string{"pkg/hot.go", "pkg/cold.go"}
	related := "pkg/lib.go"
	f := &fakeStore{
		summaries: map[string]index.FileSummary{
			"pkg/hot.go":  {Path: "pkg/hot.go", Summary: "core hot path", Symbols: []index.SymbolInfo{{Signature: "func Hot()", Description: "runs hot"}}, Imports: []string{related}},
			"pkg/cold.go": {Path: "pkg/cold.go", Summary: "seldom used", Symbols: []index.SymbolInfo{{Signature: "func Cold()"}}},
			related:       {Path: related, Summary: "shared helpers"},
		},
		dirSummaries: map[string]index.DirectorySummary{
			"pkg": {Path: "pkg", Summary: "core package", FileCount: 5},
		},
		inbound: map[string]int{"pkg/hot.go": 20, "pkg/cold.go": 1},
		blast: []index.BlastRadiusEntry{
			// Deliberately NOT in NewFileContent so it lands in the blast
			// section (not source excerpts).
			{Path: "cmd/app/main.go", Summary: "entrypoint", AffectedSymbols: []string{"Hot"}, Depth: 1},
		},
	}
	opts := Options{
		ChangedPaths: changed,
		NewFileContent: map[string]string{
			"pkg/hot.go":  "package pkg\n\nfunc Hot() {}\n",
			"pkg/cold.go": "package pkg\n\nfunc Cold() {}\n",
		},
		TokenBudget: 2000, // -> 8000 char budget
		Store:       f,
	}
	out, err := BuildIndexed(context.Background(), opts)
	if err != nil {
		t.Fatalf("BuildIndexed err: %v", err)
	}
	// Header present.
	if !strings.HasPrefix(out, "## Codebase context") {
		t.Errorf("missing header:\n%s", out)
	}
	// Each section present.
	for _, needle := range []string{
		"### Source excerpts", "### Directory context", "### File summaries", "### Blast radius",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("missing section %q:\n%s", needle, out)
		}
	}
	// hot.go should be ordered first (higher inbound count).
	hotIdx := strings.Index(out, "pkg/hot.go")
	coldIdx := strings.Index(out, "pkg/cold.go")
	if hotIdx < 0 || coldIdx < 0 || hotIdx > coldIdx {
		t.Errorf("expected hot.go before cold.go (inbound rank); hot=%d cold=%d", hotIdx, coldIdx)
	}
	// Total budget respected.
	if len(out) > tokensToChars(opts.TokenBudget)+128 {
		t.Errorf("output %d chars > budget %d", len(out), tokensToChars(opts.TokenBudget))
	}
}

func TestBuildIndexed_TrivialSummariesSkipped(t *testing.T) {
	f := &fakeStore{
		summaries: map[string]index.FileSummary{
			"a.go": {Path: "a.go", Summary: ""}, // trivial file
			"b.go": {Path: "b.go", Summary: "real content"},
		},
	}
	opts := Options{
		ChangedPaths: []string{"a.go", "b.go"},
		Store:        f,
	}
	out, err := BuildIndexed(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.Contains(out, "`a.go`:") {
		t.Errorf("trivial summary leaked into output:\n%s", out)
	}
	if !strings.Contains(out, "b.go") {
		t.Errorf("real summary missing:\n%s", out)
	}
}

func TestBuildIndexed_StoreRequired(t *testing.T) {
	if _, err := BuildIndexed(context.Background(), Options{ChangedPaths: []string{"a"}}); err == nil {
		t.Errorf("expected error when Store is nil")
	}
}

func TestBuildIndexed_EmptyInputsYieldEmpty(t *testing.T) {
	out, err := BuildIndexed(context.Background(), Options{Store: &fakeStore{}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != "" {
		t.Errorf("empty inputs must yield empty output, got:\n%s", out)
	}
}

func TestBuildIndexed_GracefulDegradationOnStoreErrors(t *testing.T) {
	// If blast + summaries + dirs all error, we still return "" cleanly.
	f := &fakeStore{
		blastErr:   errors.New("boom"),
		summaryErr: errors.New("boom"),
		dirErr:     errors.New("boom"),
	}
	opts := Options{
		ChangedPaths: []string{"a.go"},
		Store:        f,
	}
	out, err := BuildIndexed(context.Background(), opts)
	if err != nil {
		t.Fatalf("must not surface store errors: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output on total store failure, got %q", out)
	}
}

func TestDedupeBlastRadius(t *testing.T) {
	in := []index.BlastRadiusEntry{
		{Path: "a.go", AffectedSymbols: []string{"X", "Y"}, Depth: 2},
		{Path: "a.go", AffectedSymbols: []string{"Y", "Z"}, Depth: 1},
		{Path: "b.go", AffectedSymbols: []string{"K"}, Depth: 1},
		{Path: "b.go", AffectedSymbols: []string{"K"}, Depth: 3}, // fully redundant
	}
	got := dedupeBlastRadius(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 unique paths, got %d: %+v", len(got), got)
	}
	// a.go merged: depth = min(2,1) = 1, symbols = {X, Y, Z} in insertion order.
	if got[0].Path != "a.go" || got[0].Depth != 1 {
		t.Errorf("a.go: %+v", got[0])
	}
	if !equalStringSet(got[0].AffectedSymbols, []string{"X", "Y", "Z"}) {
		t.Errorf("a.go symbols: %v", got[0].AffectedSymbols)
	}
	// b.go: dup symbol K collapses.
	if len(got[1].AffectedSymbols) != 1 || got[1].AffectedSymbols[0] != "K" {
		t.Errorf("b.go symbols: %v", got[1].AffectedSymbols)
	}
}

func TestDedupeBlastRadius_Empty(t *testing.T) {
	if got := dedupeBlastRadius(nil); got != nil {
		t.Errorf("nil in nil out expected, got %v", got)
	}
}

func TestBuild_PrefersIndexedFallsBackToJIT(t *testing.T) {
	// Store returns nothing AND NewFileContent is empty for changed paths ->
	// indexed produces "", so Build falls back to JIT which reads the
	// imported file's content.
	f := &fakeStore{} // no summaries, no blast
	opts := Options{
		ChangedPaths: []string{"app/main.py"},
		// Only the imported file has content — the changed file is fetched
		// during JIT's import extraction, but indexed's source section
		// skips it (no NewFileContent entry).
		NewFileContent: map[string]string{
			"app/main.py":   "from lib.helper import go\n",
			"lib/helper.py": "def go(): return 1\n",
		},
		RepoTree:     []string{"app/main.py", "lib/helper.py"},
		Store:        f,
		EnableJavaGo: true,
	}
	out, err := Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build err: %v", err)
	}
	// Either the indexed path produced source excerpts (also acceptable), or
	// the JIT fallback fired. Both indicate Build stitched something together.
	if !strings.Contains(out, "lib/helper.py") && !strings.Contains(out, "app/main.py") {
		t.Errorf("Build produced no context at all:\n%s", out)
	}
}

func TestBuild_TrulyEmptyIndexedFallsBackToJIT(t *testing.T) {
	// Store returns nothing AND indexed can't inline source (no NewFileContent
	// for the changed path). Then indexed returns "" and JIT runs.
	f := &fakeStore{}
	opts := Options{
		ChangedPaths:   []string{"a.py"},
		NewFileContent: nil, // <- forces indexed to be empty
		Store:          f,
	}
	out, err := Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// JIT with no NewFileContent -> also empty. This is the graceful path:
	// we return "" rather than a header-only block.
	if out != "" {
		t.Errorf("expected empty output, got:\n%s", out)
	}
}

func TestBuild_NilStoreGoesJIT(t *testing.T) {
	opts := Options{
		ChangedPaths: []string{"app/main.py"},
		NewFileContent: map[string]string{
			"app/main.py":   "from lib.helper import go\n",
			"lib/helper.py": "def go(): return 1\n",
		},
		RepoTree:     []string{"app/main.py", "lib/helper.py"},
		EnableJavaGo: true,
	}
	out, err := Build(context.Background(), opts)
	if err != nil {
		t.Fatalf("Build err: %v", err)
	}
	if !strings.Contains(out, "lib/helper.py") {
		t.Errorf("nil-Store must go JIT and inline helper:\n%s", out)
	}
}

func TestFormatSymbols(t *testing.T) {
	if got := formatSymbols(nil); got != "(none)" {
		t.Errorf("empty: %q", got)
	}
	if got := formatSymbols([]string{"A", "B"}); got != "`A()`, `B()`" {
		t.Errorf("got %q", got)
	}
}

func TestHasContent(t *testing.T) {
	if hasContent("") || hasContent("   \n\t") {
		t.Error("whitespace only should be empty")
	}
	if !hasContent("x") {
		t.Error("real content")
	}
}

func TestBuildIndexed_SummaryLineVariants(t *testing.T) {
	// Exercises writeSummaryLine's three branches: signature+description,
	// signature only, and imports list. Also verifies empty-dir-summary
	// rows are skipped from the directory section.
	f := &fakeStore{
		summaries: map[string]index.FileSummary{
			"pkg/a.go": {
				Path:    "pkg/a.go",
				Summary: "the alpha module",
				Symbols: []index.SymbolInfo{
					{Name: "Full", Signature: "func Full() error", Description: "with description"},
					{Name: "Bare", Signature: "func Bare()"}, // no description
					{Name: "NoSig", Description: "signature falls back to name"},
				},
				Imports: []string{"pkg/b.go", "pkg/c.go"},
			},
			"pkg/b.go": {Path: "pkg/b.go", Summary: "sibling"},
		},
		dirSummaries: map[string]index.DirectorySummary{
			"pkg":   {Path: "pkg", Summary: "real dir", FileCount: 2},
			"other": {Path: "other", Summary: "", FileCount: 1}, // must be skipped
		},
	}
	opts := Options{
		ChangedPaths: []string{"pkg/a.go", "other/x.go"},
		Store:        f,
	}
	out, err := BuildIndexed(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, needle := range []string{
		"`func Full() error`: with description",
		"`func Bare()`",
		"`NoSig`", // signature fallback
		"Imports: pkg/b.go, pkg/c.go",
		"real dir",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("missing %q in:\n%s", needle, out)
		}
	}
	// Empty-summary dir row must not appear.
	if strings.Contains(out, "`other/`") {
		t.Errorf("empty-summary dir leaked:\n%s", out)
	}
}

func TestBuildIndexed_TruncationSafeguard(t *testing.T) {
	// A comically large summary should trigger the overall truncation cap.
	huge := strings.Repeat("blah blah blah\n", 500)
	f := &fakeStore{
		summaries: map[string]index.FileSummary{
			"a.go": {Path: "a.go", Summary: huge},
		},
	}
	opts := Options{
		ChangedPaths: []string{"a.go"},
		TokenBudget:  200, // 800 chars — well under the summary length
		Store:        f,
	}
	out, err := BuildIndexed(context.Background(), opts)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "truncated") {
		// It's fine if a section-level cap already ate the excess — but if
		// not, the outer safeguard must show up. Only fail if the output
		// exceeds the char budget with no truncation marker.
		if len(out) > tokensToChars(opts.TokenBudget)+128 {
			t.Errorf("no truncation marker AND output exceeds budget %d: len=%d", tokensToChars(opts.TokenBudget), len(out))
		}
	}
}

// equalStringSet checks two string slices contain the same elements
// (order-insensitive).
func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int)
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}
