// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package rules loads org-level review rules from a git-hosted rules
// repo, filters them by scope + path glob, and renders the injectable
// "## Custom Review Rules" block for the review prompt.
package rules

import "github.com/bmatcuk/doublestar/v4"

// Match reports whether path matches pattern. `**` matches across path
// separators (e.g. `src/**/*.ts` matches `src/a/b.ts`). Malformed patterns
// yield a non-match rather than an error.
func Match(pattern, path string) bool {
	if pattern == "" || path == "" {
		return false
	}
	ok, err := doublestar.PathMatch(pattern, path)
	if err != nil {
		return false
	}
	return ok
}

// MatchAny reports whether path matches any of patterns. Returns false for
// an empty pattern list.
func MatchAny(patterns []string, path string) bool {
	for _, p := range patterns {
		if Match(p, path) {
			return true
		}
	}
	return false
}
