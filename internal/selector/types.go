// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt
//
// Adapted from alibaba/open-code-review internal/agent/selection.go
// under Apache License 2.0. Rewritten to remove dependency on OCR's
// agent-loop scaffolding (Template, PromptTokenLimit, session identity)
// and to introduce the Decision/Reason surface for pipeline reporting.

// Package selector applies the deterministic pre-dispatch file gate for the
// review pipeline: binary/user-glob/extension/default-path/size checks. The
// pass is pure — no I/O, no goroutines, no randomness — and emits one
// Decision per input diff so nothing is dropped silently.
package selector

import "github.com/shubam-disseqt/z-code-reviewer/internal/model"

// ExclusionReason names the specific gate that rejected a diff, or is empty
// when the diff was kept.
type ExclusionReason string

// Reasons a diff can be excluded. ReasonNone means "included."
const (
	ReasonNone           ExclusionReason = ""
	ReasonBinary         ExclusionReason = "binary"
	ReasonUserExclude    ExclusionReason = "user_exclude"
	ReasonNotAllowed     ExclusionReason = "not_allowed"
	ReasonExtension      ExclusionReason = "extension_not_supported"
	ReasonDefaultPattern ExclusionReason = "default_pattern"
	ReasonDeleted        ExclusionReason = "deleted"
	ReasonTooLarge       ExclusionReason = "too_large"
)

// Options configures Select. The zero value is valid — see DefaultOptions
// for the recommended baseline.
type Options struct {
	// MaxTokensPerFile is the per-file token cap. A diff whose token count
	// exceeds this cap gets ReasonTooLarge. Zero disables the size gate.
	MaxTokensPerFile int

	// IncludePatterns is an allowlist of repo-relative doublestar globs.
	// Empty means "no allowlist filter." When non-empty, a diff whose path
	// matches none of these gets ReasonNotAllowed and skips the extension
	// and default-pattern gates below it — an explicit include wins.
	IncludePatterns []string

	// ExcludePatterns is a user denylist of repo-relative doublestar globs.
	// A match yields ReasonUserExclude.
	ExcludePatterns []string

	// ExcludeDeleted drops deleted-file diffs with ReasonDeleted.
	ExcludeDeleted bool

	// TokenCounter overrides the default byte/4 estimator. Callers can pass
	// llm.CountTokens for the real tokenizer. Nil uses the default.
	TokenCounter func(string) int
}

// DefaultOptions returns a reasonable baseline: a 12k-token per-file cap,
// deletions kept, no user globs, default token estimator.
func DefaultOptions() Options {
	return Options{
		MaxTokensPerFile: 12000,
		ExcludeDeleted:   false,
	}
}

// Decision is the outcome for one input diff. Reason is empty exactly when
// Included is true.
type Decision struct {
	Diff     model.Diff
	Included bool
	Reason   ExclusionReason
}
