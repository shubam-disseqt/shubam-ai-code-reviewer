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
var (
	linePrefixRE = regexp.MustCompile(`(?m)^\s*\d+\s*[:|]?\s*`)
	wsRunRE      = regexp.MustCompile(`\s+`)
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
	s = stripLineComments(s)
	s = wsRunRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// stripLineComments removes trailing // or # line comments while skipping
// occurrences inside string literals ("...", '...', `...`, """...""") and
// inside URL schemes (http://, git+ssh://, etc.). It is a heuristic scanner,
// not a full tokenizer: escaped quotes inside strings are handled, but
// language-specific corners (template literals with ${…}, nested Python
// f-string braces) are intentionally out of scope. Both sides of a compare
// get the same treatment, so mild imprecision preserves equality.
func stripLineComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(stripLineCommentOne(line))
	}
	return b.String()
}

// stripLineCommentOne scans a single line and returns it with any trailing
// line-comment (// or #) removed. See stripLineComments for scope notes.
func stripLineCommentOne(line string) string {
	inSingle, inDouble, inBacktick, inTriple := false, false, false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		// Handle escapes inside single/double quoted strings (skip next byte).
		if (inSingle || inDouble) && c == '\\' && i+1 < len(line) {
			i++
			continue
		}
		switch {
		case inTriple:
			if c == '"' && i+2 < len(line) && line[i+1] == '"' && line[i+2] == '"' {
				inTriple = false
				i += 2
			}
		case inBacktick:
			if c == '`' {
				inBacktick = false
			}
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			}
		default:
			if c == '"' && i+2 < len(line) && line[i+1] == '"' && line[i+2] == '"' {
				inTriple = true
				i += 2
				continue
			}
			if c == '"' {
				inDouble = true
				continue
			}
			if c == '\'' {
				inSingle = true
				continue
			}
			if c == '`' {
				inBacktick = true
				continue
			}
			if c == '#' {
				return line[:i]
			}
			if c == '/' && i+1 < len(line) && line[i+1] == '/' {
				if isURLSchemeAt(line, i) {
					i++ // skip second slash, stay in code
					continue
				}
				return line[:i]
			}
		}
	}
	return line
}

// isURLSchemeAt reports whether the "//" at position i in line is the
// scheme separator of a URL (e.g. http://, https://, git://, git+ssh://).
// A scheme is a run of lowercase letters, digits, '+', '-', '.' immediately
// before ":" — cheap to check and covers the common cases.
func isURLSchemeAt(line string, i int) bool {
	if i < 2 || line[i-1] != ':' {
		return false
	}
	j := i - 2
	for j >= 0 && isSchemeByte(line[j]) {
		j--
	}
	// At least one scheme byte, and first byte of the scheme must be a
	// lowercase letter (per RFC 3986: scheme = ALPHA *( ALPHA / DIGIT / …)).
	start := j + 1
	if start >= i-1 {
		return false
	}
	first := line[start]
	return first >= 'a' && first <= 'z'
}

func isSchemeByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '+' || c == '-' || c == '.':
		return true
	}
	return false
}
