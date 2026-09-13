// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/context.py under
// Apache License 2.0.

package reviewctx

// CountTokens estimates the token count for s using Mira's 4-chars-per-token
// heuristic. This intentionally avoids pulling in a tiktoken dependency —
// the caller uses this to slice a budget, and the exact BPE count doesn't
// change how much markdown fits. The budgets themselves come from Mira and
// were tuned against this same approximation.
func CountTokens(s string) int {
	if s == "" {
		return 0
	}
	return len(s) / charsPerToken
}

// tokensToChars converts a token budget to a byte budget using the same
// conservative 4-chars-per-token ratio Mira uses internally.
func tokensToChars(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	return tokens * charsPerToken
}
