// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 shubam-ai-code-reviewer contributors
//
// Adapted from alibaba/open-code-review internal/agent/selection.go
// under Apache License 2.0. Rewritten to remove dependency on sacr's
// agent-loop scaffolding (Template, PromptTokenLimit, session identity)
// and to introduce the Decision/Reason surface for pipeline reporting.

package selector

import "github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"

// defaultTokenCounter is the fallback token estimator: one token per four
// bytes. Cheap, deterministic, no allocation. Callers wanting the real model
// tokenizer should pass Options.TokenCounter = llm.CountTokens.
func defaultTokenCounter(s string) int {
	if s == "" {
		return 0
	}
	// Ceil(len/4) so a one-byte diff still counts as one token.
	return (len(s) + 3) / 4
}

// Select applies the deterministic pre-dispatch gate to diffs in input order.
// Every input yields exactly one Decision — no silent drops — which is the
// invariant preview and dispatch share so they can never disagree.
//
// The pass is pure: no I/O, no goroutines, no randomness. Given the same
// diffs and Options it returns byte-identical output.
func Select(diffs []model.Diff, opts Options) []Decision {
	counter := opts.TokenCounter
	if counter == nil {
		counter = defaultTokenCounter
	}
	decisions := make([]Decision, 0, len(diffs))
	for _, d := range diffs {
		reason := whyExcluded(d, opts)
		if reason == ReasonNone && opts.MaxTokensPerFile > 0 {
			if counter(d.Diff) > opts.MaxTokensPerFile {
				reason = ReasonTooLarge
			}
		}
		decisions = append(decisions, Decision{
			Diff:     d,
			Included: reason == ReasonNone,
			Reason:   reason,
		})
	}
	return decisions
}

// Kept returns only the diffs whose decisions were Included, in input order.
func Kept(decisions []Decision) []model.Diff {
	out := make([]model.Diff, 0, len(decisions))
	for _, dec := range decisions {
		if dec.Included {
			out = append(out, dec.Diff)
		}
	}
	return out
}

// Summary counts decisions by reason. The included bucket lives under
// ReasonNone so every decision is accounted for and the total matches the
// input length exactly.
func Summary(decisions []Decision) map[ExclusionReason]int {
	m := make(map[ExclusionReason]int, 8)
	for _, dec := range decisions {
		m[dec.Reason]++
	}
	return m
}
