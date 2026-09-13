// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package rules

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"src/**/*.ts", "src/a/b.ts", true},
		{"src/**/*.ts", "src/a/b/c/d.ts", true},
		{"src/**/*.ts", "lib/a.ts", false},
		{"**/*.test.ts", "src/a/b.test.ts", true},
		{"**/*.test.ts", "src/a/b.ts", false},
		{"*.go", "main.go", true},
		{"*.go", "cmd/main.go", false},
		{"docs/*.md", "docs/index.md", true},
		{"docs/*.md", "docs/sub/index.md", false},
		{"", "a.go", false},
		{"*.go", "", false},
		{"[unterminated", "x", false}, // malformed → non-match
	}
	for _, tc := range cases {
		t.Run(tc.pattern+"|"+tc.path, func(t *testing.T) {
			got := Match(tc.pattern, tc.path)
			if got != tc.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestMatchAny(t *testing.T) {
	if MatchAny(nil, "x.go") {
		t.Error("nil patterns should not match")
	}
	if MatchAny([]string{}, "x.go") {
		t.Error("empty patterns should not match")
	}
	if !MatchAny([]string{"*.js", "*.go"}, "x.go") {
		t.Error("expected match on second pattern")
	}
	if MatchAny([]string{"*.js", "*.py"}, "x.go") {
		t.Error("expected no match")
	}
}
