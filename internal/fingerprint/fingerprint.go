// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package fingerprint computes a stable per-finding identity that survives
// whitespace-only edits, comment-only edits, and small formatting churn so
// incremental re-reviews can carry state across pushes.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"regexp"
	"strings"
)

// Input is the material a Fingerprint hashes over. Fields map 1:1 to the
// documented tuple in ARCHITECTURE §3:
// sha256(owner|repo|category|normalized_path|symbol|normalized_snippet).
type Input struct {
	Owner    string
	Repo     string
	Category string
	Path     string
	Symbol   string
	Snippet  string
}

// Fingerprint returns the hex sha256 of the pipe-joined normalized fields.
//
// Normalization rules (see fingerprint_test.go for the exhaustive matrix):
//   - path: lower-cased file extension; separators kept as-is
//   - snippet: line-number prefixes stripped, // and # line comments dropped,
//     whitespace runs collapsed to a single space, leading/trailing space trimmed
//   - other fields: trimmed only (they're identity, not content)
func Fingerprint(f Input) string {
	parts := []string{
		strings.TrimSpace(f.Owner),
		strings.TrimSpace(f.Repo),
		strings.TrimSpace(f.Category),
		normalizePath(f.Path),
		strings.TrimSpace(f.Symbol),
		normalizeSnippet(f.Snippet),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// normalizePath lower-cases the extension so a rename from .JS to .js
// (or Windows→Unix casing quirks) doesn't flap the fingerprint. The rest
// of the path is preserved because a real rename SHOULD change identity.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	ext := path.Ext(p)
	if ext == "" {
		return p
	}
	return strings.TrimSuffix(p, ext) + strings.ToLower(ext)
}

// linePrefix matches leading line-number markers like "12:", "  42 |",
// "  42  ", used by various tools (grep, ripgrep, diff snippets). We strip
// them so "12: foo()" and "13: foo()" fingerprint identically.
//
// ponytail: naive comment stripping — eats "//" inside strings or URLs.
// Both sides of the compare get the same treatment, so equality is preserved;
// upgrade to a token-aware pass only if false-matches cause pain in practice.
var (
	linePrefixRE  = regexp.MustCompile(`(?m)^\s*\d+\s*[:|]?\s*`)
	lineCommentRE = regexp.MustCompile(`(?m)(//|#).*$`)
	wsRunRE       = regexp.MustCompile(`\s+`)
)

// normalizeSnippet strips the noise that pure formatting/comment edits
// introduce and returns a canonical form. Order matters: strip line prefixes
// FIRST so we don't accidentally match "12:" as content, then comments, then
// collapse whitespace.
func normalizeSnippet(s string) string {
	if s == "" {
		return ""
	}
	s = linePrefixRE.ReplaceAllString(s, "")
	s = lineCommentRE.ReplaceAllString(s, "")
	s = wsRunRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
