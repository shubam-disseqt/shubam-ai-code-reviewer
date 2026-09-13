// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package index

import (
	"strings"
	"testing"
)

func TestShouldIndex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		path  string
		extra []string
		want  bool
	}{
		{"go file", "cmd/main.go", nil, true},
		{"python file at root", "app.py", nil, true},
		{"nested typescript", "src/lib/index.ts", nil, true},
		{"lockfile by basename", "backend/package-lock.json", nil, false},
		{"go.sum by basename", "go.sum", nil, false},
		{"min.js", "static/vendor.min.js", nil, false},
		{"node_modules prefix", "node_modules/foo/index.js", nil, false},
		{"vendor prefix", "vendor/pkg/foo.go", nil, false},
		{"unknown extension", "README.docx", nil, false},
		{"markdown not indexable", "docs/intro.md", nil, false}, // md is not in indexable list
		{"user exclude glob", "generated/foo.go", []string{"generated/*"}, false},
		{"user exclude basename", "internal/mocks.go", []string{"mocks.go"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldIndex(tc.path, tc.extra)
			if got != tc.want {
				t.Errorf("shouldIndex(%q, %v) = %v, want %v", tc.path, tc.extra, got, tc.want)
			}
		})
	}
}

func TestBuildBatchesTrivialSmallLarge(t *testing.T) {
	t.Parallel()
	// 1 large file (>=5000 bytes) — solo batch.
	large := filePair{Path: "big.go", Content: strings.Repeat("a", largeFileBytes+1)}
	// 5 small files — pack into batches of 3.
	small := make([]filePair, 5)
	for i := range small {
		small[i] = filePair{Path: string(rune('a'+i)) + ".go", Content: "package x"}
	}
	pairs := append([]filePair{large}, small...)
	batches := buildBatches(pairs)

	// Expected: [large], [small[0..3]], [small[3..5]]
	if got, want := len(batches), 3; got != want {
		t.Fatalf("got %d batches, want %d", got, want)
	}
	if len(batches[0]) != 1 || batches[0][0].Path != "big.go" {
		t.Errorf("first batch should be solo large: got %+v", batches[0])
	}
	if len(batches[1]) != 3 {
		t.Errorf("second batch size = %d, want 3", len(batches[1]))
	}
	if len(batches[2]) != 2 {
		t.Errorf("third batch size = %d, want 2", len(batches[2]))
	}
}

func TestBuildBatchesEmpty(t *testing.T) {
	t.Parallel()
	if got := buildBatches(nil); got != nil {
		t.Errorf("buildBatches(nil) = %v, want nil", got)
	}
}

func TestIsTrivial(t *testing.T) {
	t.Parallel()
	if !isTrivial(strings.Repeat("x", trivialFileBytes-1)) {
		t.Errorf("just-under threshold should be trivial")
	}
	if isTrivial(strings.Repeat("x", trivialFileBytes)) {
		t.Errorf("exactly-at threshold should NOT be trivial (semantics: <)")
	}
	if isTrivial("") {
		// Empty is trivially trivial — Mira persists an empty row.
		t.Log("empty is trivial (expected)")
	}
}

func TestContentHashStable(t *testing.T) {
	t.Parallel()
	a := contentHash("hello world")
	b := contentHash("hello world")
	if a != b {
		t.Errorf("contentHash not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("contentHash length = %d, want 64 (hex sha256)", len(a))
	}
	if contentHash("hello") == contentHash("world") {
		t.Errorf("contentHash collided across distinct inputs")
	}
}

func TestContentHashReplacesInvalidUTF8(t *testing.T) {
	t.Parallel()
	// Invalid byte 0xFF replaced with U+FFFD. Two inputs sharing invalid
	// bytes at the same position should hash the same.
	invalid1 := "abc" + string([]byte{0xFF, 0xFE}) + "xyz"
	invalid2 := "abc" + string([]byte{0xFF, 0xFE}) + "xyz"
	if contentHash(invalid1) != contentHash(invalid2) {
		t.Errorf("hash of same invalid bytes differs")
	}
	// Valid text with U+FFFD should equal the replaced-invalid case.
	replaced := "abc" + "��" + "xyz"
	if contentHash(invalid1) != contentHash(replaced) {
		t.Errorf("invalid utf-8 not normalized to U+FFFD: %s vs %s", contentHash(invalid1), contentHash(replaced))
	}
}

func TestCountLOC(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"one line\n", 1},
		{"one line no newline", 1},
		{"a\nb\nc\n", 3},
		{"a\nb\nc", 3},
	}
	for _, tc := range cases {
		if got := countLOC(tc.in); got != tc.want {
			t.Errorf("countLOC(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestMatchGlobPrefix(t *testing.T) {
	t.Parallel()
	// Prefix-directory case that path.Match doesn't match by itself.
	if !matchGlob("node_modules/*", "node_modules/foo/bar/baz.js") {
		t.Errorf("node_modules/* should match deep child")
	}
	if matchGlob("node_modules/*", "not_node_modules/foo.js") {
		t.Errorf("node_modules/* must not match unrelated prefix")
	}
	// Extension case handled by path.Match directly.
	if !matchGlob("*.lock", "yarn.lock") {
		t.Errorf("*.lock should match yarn.lock")
	}
}
