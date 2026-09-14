// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package effort

import (
	"path/filepath"
	"strings"
)

// Path buckets each contribute a flat weight when ANY changed file matches.
// Matching is component-based (path split on `/`) not substring — so
// `internal/user/authors.go` doesn't wrongly trigger the auth bucket.

// authPathKeywords name path components that imply security-sensitive code.
// Match is case-insensitive and exact-per-component.
var authPathKeywords = []string{
	"auth", "authn", "authz", "session", "sessions", "token", "tokens",
	"crypto", "oauth", "oauth2", "jwt", "tls", "ssl", "permission",
	"permissions", "rbac", "acl", "iam", "keycloak",
}

// migrationPathKeywords name path components that imply schema or data
// migration work. The reviewer has to check backfill safety, rollback, and
// production ordering — always more effort than a code change.
var migrationPathKeywords = []string{
	"migration", "migrations", "migrate", "schema", "schemata", "sqlmigrate",
}

// infraPathKeywords name path components that imply infrastructure or
// deployment change surface. Wrong here = production outage.
var infraPathKeywords = []string{
	"dockerfile", "docker", "k8s", "kubernetes", "helm", "terraform",
	"ansible", "cloudformation", "pulumi",
}

// TouchesAuth reports whether ANY of files matches an auth path component
// or an auth file suffix (`.sql` migration files also count as migration,
// not auth).
func TouchesAuth(files []FileDelta) bool {
	for _, f := range files {
		if anyComponentMatches(f.Path, authPathKeywords) {
			return true
		}
	}
	return false
}

// TouchesMigration reports whether any file is a database migration. Path-
// component match plus `.sql` extension.
func TouchesMigration(files []FileDelta) bool {
	for _, f := range files {
		if anyComponentMatches(f.Path, migrationPathKeywords) {
			return true
		}
		if strings.EqualFold(filepath.Ext(f.Path), ".sql") {
			return true
		}
	}
	return false
}

// TouchesInfra reports whether any file is infrastructure code.
func TouchesInfra(files []FileDelta) bool {
	for _, f := range files {
		base := strings.ToLower(filepath.Base(f.Path))
		if base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") {
			return true
		}
		if anyComponentMatches(f.Path, infraPathKeywords) {
			return true
		}
	}
	return false
}

// IsTestFile reports whether the path is a test file by convention across
// Go, TypeScript/JavaScript, Python, and Rust. Best-effort — a false
// negative just skips the test-ratio bonus, never breaks the score.
func IsTestFile(path string) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	switch {
	case strings.HasSuffix(lower, "_test.go"): // Go
		return true
	case strings.HasSuffix(lower, ".test.js"), strings.HasSuffix(lower, ".test.ts"),
		strings.HasSuffix(lower, ".test.jsx"), strings.HasSuffix(lower, ".test.tsx"),
		strings.HasSuffix(lower, ".spec.js"), strings.HasSuffix(lower, ".spec.ts"):
		return true
	case strings.HasSuffix(lower, "_test.py"),
		strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py"):
		return true
	}
	// Directory-based fallback: file lives under a `test`, `tests`, or
	// `__tests__` directory.
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts[:max(0, len(parts)-1)] {
		lp := strings.ToLower(p)
		if lp == "test" || lp == "tests" || lp == "__tests__" || lp == "spec" {
			return true
		}
	}
	return false
}

// anyComponentMatches returns true if ANY of the slash-separated components
// of path (excluding the file name) is an exact case-insensitive match for
// one of the keywords. The file-name basename is included in the check for
// keywords like `dockerfile`, but only when the WHOLE basename matches —
// substrings inside a normal file name (`internal/user/authors.go`) never
// trigger.
func anyComponentMatches(path string, keywords []string) bool {
	parts := strings.Split(strings.ToLower(filepath.ToSlash(path)), "/")
	for _, part := range parts {
		// Strip file extension so `session.go` matches "session".
		part = strings.TrimSuffix(part, filepath.Ext(part))
		for _, kw := range keywords {
			if part == kw {
				return true
			}
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
