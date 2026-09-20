// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 shubam-ai-code-reviewer contributors
//
// Adapted from alibaba/open-code-review internal/agent/selection.go
// under Apache License 2.0.

package selector

import (
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/filetype"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// effectivePath is the path we filter against. For a deletion (NewPath is
// /dev/null) we fall back to OldPath so the diff still has a real name.
func effectivePath(d model.Diff) string {
	if d.NewPath == "" || d.NewPath == "/dev/null" {
		return d.OldPath
	}
	return d.NewPath
}

// whyExcluded runs the static gates in order and returns the specific reason
// a diff is excluded, or ReasonNone when it survives. The size gate lives in
// Select — it needs the resolved token counter and limit.
//
// Order matters:
//  1. binary
//  2. deleted (only when opts.ExcludeDeleted)
//  3. user exclude glob
//  4. user include allowlist — non-empty allowlist short-circuits: a match
//     wins outright, a miss rejects immediately (no fallthrough to the
//     extension/default gates).
//  5. extension not indexable
//  6. default path denylist
func whyExcluded(d model.Diff, opts Options) ExclusionReason {
	if d.IsBinary {
		return ReasonBinary
	}
	if d.IsDeleted && opts.ExcludeDeleted {
		return ReasonDeleted
	}
	path := effectivePath(d)
	if len(opts.ExcludePatterns) > 0 && matchesAny(path, opts.ExcludePatterns) {
		return ReasonUserExclude
	}
	if len(opts.IncludePatterns) > 0 {
		if !matchesAny(path, opts.IncludePatterns) {
			return ReasonNotAllowed
		}
		// Explicit include: skip the extension and default-path gates.
		return ReasonNone
	}
	if !filetype.IsIndexablePath(path) {
		return ReasonExtension
	}
	if matchesDefaultSkip(path) {
		return ReasonDefaultPattern
	}
	return ReasonNone
}
