// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/conventions.py under Apache License 2.0.

// Package conventions loads and normalizes a repository's contributor-facing
// convention docs (AGENTS.md, CONTRIBUTING.md, CLAUDE.md, .cursor/rules/*.md)
// so they can be injected into a review prompt.
package conventions

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MaxTotalChars caps the combined output. ~4 chars per token puts this at
// roughly 2K prompt tokens — enough for real style guidance, not enough to
// dominate the review budget.
const MaxTotalChars = 8000

// maxPerFileChars caps a single source file before boilerplate stripping so a
// giant CONTRIBUTING.md can't consume the whole budget.
const maxPerFileChars = 4000

// topLevelFiles are the fixed files at the repo root, in priority order.
// Earlier entries win when the char cap is hit.
var topLevelFiles = []string{
	"AGENTS.md",
	"CONTRIBUTING.md",
	"CLAUDE.md",
}

// cursorRulesDir holds project-specific rules for the Cursor editor.
const cursorRulesDir = ".cursor/rules"

// boilerplateHeaders matches Markdown H1-H3 lines whose text is process/setup
// noise rather than coding rules. Case-insensitive, anchored at start of line.
var boilerplateHeaders = regexp.MustCompile(
	`(?i)^#{1,3}\s+(table of contents|toc|license|prerequisites|installation|` +
		`getting started|setup|how to run|running|build|deploy|` +
		`code of conduct|contributors|acknowledg(e?)ments|how to contribute|` +
		`reporting (issues|bugs)|filing (issues|bugs)|pull requests?|` +
		`opening (a )?pull request|getting help)\b`,
)

// headerLevel matches any Markdown ATX header and captures its `#`s.
var headerLevel = regexp.MustCompile(`^(#{1,6})\s+`)

// Load reads every known convention file under repoRoot, strips boilerplate,
// and returns a single concatenated string. Missing files are skipped; the
// returned string is empty if none are present.
//
// Errors from reading a file that does exist (permissions, IO) are wrapped
// and returned. A missing file is not an error.
func Load(repoRoot string) (string, error) {
	paths, err := discover(repoRoot)
	if err != nil {
		return "", err
	}

	var parts []string
	used := 0
	for _, p := range paths {
		content, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("read %s: %w", p, err)
		}
		body := strings.TrimSpace(stripBoilerplate(string(content)))
		if body == "" {
			continue
		}
		if len(body) > maxPerFileChars {
			body = body[:maxPerFileChars] + "\n…(truncated)"
		}

		rel, err := filepath.Rel(repoRoot, p)
		if err != nil {
			rel = p
		}
		section := fmt.Sprintf("### From `%s`\n\n%s", filepath.ToSlash(rel), body)

		// +2 for the "\n\n" joiner we'll add between sections.
		next := used + len(section)
		if len(parts) > 0 {
			next += 2
		}
		if next > MaxTotalChars {
			break
		}
		parts = append(parts, section)
		used = next
	}
	return strings.Join(parts, "\n\n"), nil
}

// discover returns the ordered set of convention files present under
// repoRoot. Missing files are simply omitted. Only "does the directory
// exist / can we list it" errors escape.
func discover(repoRoot string) ([]string, error) {
	var out []string
	for _, name := range topLevelFiles {
		out = append(out, filepath.Join(repoRoot, name))
	}

	rulesDir := filepath.Join(repoRoot, cursorRulesDir)
	entries, err := os.ReadDir(rulesDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", rulesDir, err)
	}
	// Stable, name-sorted iteration so output is deterministic across
	// filesystems.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		out = append(out, filepath.Join(rulesDir, e.Name()))
	}
	return out, nil
}

// stripBoilerplate drops known-boilerplate H1-H3 sections. When a matching
// header is seen, everything is dropped until the next header of the same or
// higher (numerically smaller) level.
func stripBoilerplate(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	skipUntilLevel := 0 // 0 means "not skipping"

	for _, line := range lines {
		level := 0
		if m := headerLevel.FindStringSubmatch(line); m != nil {
			level = len(m[1])
		}

		if skipUntilLevel != 0 {
			if level != 0 && level <= skipUntilLevel {
				skipUntilLevel = 0
				// Fall through and reconsider this header.
			} else {
				continue
			}
		}

		if level != 0 && boilerplateHeaders.MatchString(line) {
			skipUntilLevel = level
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
