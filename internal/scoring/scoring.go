// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scoring

import (
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// scannerConfidenceBump is added to the policy's confidence_floor when the
// finding came from a deterministic scanner (Source="scanner:<tool>").
// Scanners have near-perfect precision on the categories they flag, so
// they get a small nudge above the LLM baseline.
const scannerConfidenceBump = 0.05

// llmTagConfidenceBump is added when the LLM finding carries a specific
// rule tag (Category contains a "." — e.g. "security.hardcoded-secret"),
// which means the model matched a known policy rule rather than guessing
// at a generic bucket.
const llmTagConfidenceBump = 0.05

// confidenceCeiling clamps the final confidence at 1.0. Combined with the
// bumps above this is a defensive guard, not a real edge case in practice.
const confidenceCeiling = 1.0

// ScoreOne applies the policy to a single finding and returns the
// deterministic ScoredComment. The rules are:
//
//   - Match a policy entry by "category.rule" first (specific), then by
//     bare "category" (generic bucket), then fall back to Defaults.
//   - Compute Severity from the entry's severity_map using the finding's
//     lowercase raw severity. If the raw severity is empty or not mapped,
//     use the entry's "default" key.
//   - Confidence starts at the entry's ConfidenceFloor. Scanner findings
//     get +scannerConfidenceBump. LLM findings with a category-qualified
//     tag (contains ".") get +llmTagConfidenceBump. Result clamped at 1.
//   - Impact is the entry's Impact verbatim in v1.
//
// Rationale names the matched policy key ("secret", "security.missing-auth",
// or "defaults") so downstream consumers can trace the decision.
func ScoreOne(c model.LlmComment, p Policy) ScoredComment {
	category := strings.ToLower(strings.TrimSpace(c.Category))
	rawSev := strings.ToLower(strings.TrimSpace(c.Severity))

	key, entry := resolveEntry(p, category)

	sev := lookupSeverity(entry.SeverityMap, rawSev)

	confidence := entry.ConfidenceFloor
	if isScannerSource(c.Source) {
		confidence += scannerConfidenceBump
	} else if isTaggedLLM(category) {
		confidence += llmTagConfidenceBump
	}
	if confidence > confidenceCeiling {
		confidence = confidenceCeiling
	}

	return ScoredComment{
		LlmComment: c,
		Score: Score{
			Severity:   sev,
			Confidence: confidence,
			Impact:     entry.Impact,
			Rationale:  key,
		},
	}
}

// ScoreAll applies ScoreOne to every comment. Returned slice preserves
// input order so callers can zip results against the original comment
// list if they need to.
func ScoreAll(p Policy, comments []model.LlmComment) []ScoredComment {
	out := make([]ScoredComment, len(comments))
	for i, c := range comments {
		out[i] = ScoreOne(c, p)
	}
	return out
}

// resolveEntry returns the policy entry to use for the given category
// string and the key that matched (for the Rationale field). Fallback
// order: "category.rule" → "category" → "defaults".
//
// The input category is expected to be lowercased already. Empty or
// unknown categories fall through to defaults.
func resolveEntry(p Policy, category string) (string, PolicyEntry) {
	if category == "" {
		return "defaults", p.Defaults
	}
	if entry, ok := p.Categories[category]; ok && len(entry.SeverityMap) > 0 {
		return category, entry
	}
	// Try the bare category prefix (everything before the first ".").
	// This is what routes "security.hardcoded-secret" → "security" if
	// the operator hasn't defined the specific rule.
	if i := strings.Index(category, "."); i > 0 {
		prefix := category[:i]
		if entry, ok := p.Categories[prefix]; ok && len(entry.SeverityMap) > 0 {
			return prefix, entry
		}
	}
	return "defaults", p.Defaults
}

// lookupSeverity picks the Severity for the raw severity label. Empty or
// unmapped labels fall through to the "default" key. Missing "default"
// silently returns SUPPRESS — this only fires on a malformed policy;
// LoadPolicy validates defaults have a "default" key.
func lookupSeverity(m map[string]Severity, raw string) Severity {
	if raw != "" {
		if sev, ok := m[raw]; ok && sev != "" {
			return sev
		}
	}
	if sev, ok := m["default"]; ok && sev != "" {
		return sev
	}
	return SeveritySuppress
}

// isScannerSource reports whether the LlmComment came from a scanner
// adapter (Source="scanner:<tool>"). Matches Phase 14's contract.
func isScannerSource(src string) bool {
	return strings.HasPrefix(src, "scanner:")
}

// isTaggedLLM reports whether the LLM assigned a category-qualified tag
// (e.g. "security.hardcoded-secret") rather than a bare bucket. Presence
// of a "." is a proxy for "matched a specific rule".
func isTaggedLLM(category string) bool {
	return strings.Contains(category, ".")
}
