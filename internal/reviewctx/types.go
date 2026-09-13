// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/{jit_context,context}.py
// under Apache License 2.0.

// Package reviewctx builds the "codebase context" markdown block that gets
// injected into the review prompt. Two modes:
//
//   - Indexed mode (Store non-nil): pulls file/dir summaries + blast radius
//     from the SQLite index, splits a token budget 60/30/10 across source
//     excerpts / summaries / directory context.
//   - JIT mode (Store nil, or Store lookups return nothing useful): parses
//     each changed file's imports at HEAD, resolves candidate paths, filters
//     against the repo tree, and inlines source excerpts (capped per-file
//     and total).
//
// Build() decides. BuildJIT / BuildIndexed are exposed for testing.
package reviewctx

import (
	"github.com/shubam-disseqt/z-code-reviewer/internal/index"
)

// Defaults match Mira's constants in jit_context.py + context.py.
const (
	DefaultMaxFiles        = 8      // _MAX_FILES
	DefaultMaxPerFileChars = 1500   // _MAX_PER_FILE_CHARS
	DefaultCharBudget      = 12_000 // JIT total
	DefaultTokenBudget     = 8_000  // indexed total (_DEFAULT_TOKEN_BUDGET)
	// Mira uses "conservative estimate": 4 chars per token.
	charsPerToken = 4
)

// Options configures a context build.
type Options struct {
	// RepoRoot is the on-disk repo root (used only for logging; content is
	// supplied via NewFileContent).
	RepoRoot string

	// ChangedPaths is the set of repo-relative paths modified in the PR.
	ChangedPaths []string

	// NewFileContent maps a changed path to its source at HEAD. The caller
	// is responsible for populating this — reviewctx does no fetching.
	NewFileContent map[string]string

	// MaxFiles caps how many candidate files JIT fetches. 0 -> DefaultMaxFiles.
	MaxFiles int

	// MaxPerFileChars caps the per-file excerpt size (JIT). 0 -> DefaultMaxPerFileChars.
	MaxPerFileChars int

	// CharBudget caps total JIT output. 0 -> DefaultCharBudget.
	CharBudget int

	// EnableJavaGo toggles Java and Go JIT import resolution. Their heuristics
	// are noisy on some repos, so callers can A/B disable. Default: true.
	EnableJavaGo bool

	// TokenBudget is the indexed-mode token budget. 0 -> DefaultTokenBudget.
	// Split 60% source excerpts / 30% file summaries / 10% dir summaries.
	TokenBudget int

	// Store is the index backend. When nil, Build falls back to JIT.
	Store index.Store

	// RepoTree, when non-empty, filters JIT import candidates so we only try
	// paths that actually exist. Nil means "no filter".
	RepoTree []string
}

// resolved returns a copy of o with zero-value knobs replaced by defaults.
func (o Options) resolved() Options {
	out := o
	if out.MaxFiles == 0 {
		out.MaxFiles = DefaultMaxFiles
	}
	if out.MaxPerFileChars == 0 {
		out.MaxPerFileChars = DefaultMaxPerFileChars
	}
	if out.CharBudget == 0 {
		out.CharBudget = DefaultCharBudget
	}
	if out.TokenBudget == 0 {
		out.TokenBudget = DefaultTokenBudget
	}
	return out
}
