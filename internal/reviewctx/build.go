// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/{jit_context,context}.py
// under Apache License 2.0.

package reviewctx

import "context"

// Build produces the markdown block to inject under the review prompt. It
// prefers the indexed path when a Store is configured, and augments (or
// falls back entirely to) JIT extraction when the index yields nothing.
//
// Empty output ("") is a valid, non-error result — the caller should skip
// the section header rather than emitting an empty block.
func Build(ctx context.Context, opts Options) (string, error) {
	if opts.Store != nil {
		out, err := BuildIndexed(ctx, opts)
		if err != nil {
			return "", err
		}
		if out != "" {
			return out, nil
		}
		// Fall through: index had no useful context for these files
		// (fresh install, cache miss, all summaries trivial).
	}
	return BuildJIT(ctx, opts)
}
