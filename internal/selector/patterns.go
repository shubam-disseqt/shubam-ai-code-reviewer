// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt
//
// Adapted from alibaba/open-code-review internal/agent/selection.go
// under Apache License 2.0.

package selector

import "github.com/bmatcuk/doublestar/v4"

// defaultSkipPatterns are the built-in path denylist: vendor/build outputs,
// lockfiles, minified/map bundles, VCS metadata, and Python bytecode caches.
// Matched with doublestar semantics so `**/` crosses any depth.
var defaultSkipPatterns = []string{
	"**/vendor/**",
	"**/node_modules/**",
	"**/dist/**",
	"**/build/**",
	"**/*.lock",
	"package-lock.json",
	"**/package-lock.json",
	"yarn.lock",
	"**/yarn.lock",
	"**/*.min.js",
	"**/*.min.css",
	"**/*.map",
	"**/.git/**",
	"**/__pycache__/**",
}

// matchesAny reports whether path matches any doublestar pattern. Malformed
// patterns are treated as non-matches — this is a gate, not a validator.
func matchesAny(path string, patterns []string) bool {
	for _, pat := range patterns {
		if ok, err := doublestar.PathMatch(pat, path); err == nil && ok {
			return true
		}
	}
	return false
}

// matchesDefaultSkip reports whether path hits the built-in denylist.
func matchesDefaultSkip(path string) bool {
	return matchesAny(path, defaultSkipPatterns)
}
