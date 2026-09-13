// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package scoring is the deterministic severity policy that turns raw
// findings (LLM comments + scanner findings) into a Score containing a
// bucketed Severity, a confidence in [0,1], and an impact in [0,1].
//
// The policy is table-driven from an embedded YAML default, overridable
// via ZREVIEW_SCORING_POLICY or a repo-local .zreview/scoring.yaml.
// There is no reflection loop — CR-bench (NUS, 2026) measured that
// reflexion hurts review usefulness. See ARCHITECTURE §4.
package scoring

import (
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
)

// Severity is the bucketed severity produced by the scoring engine.
// SUPPRESS is the "drop this finding" sentinel; the emit stage filters
// SUPPRESS unconditionally, regardless of --min-severity.
type Severity string

const (
	SeveritySuppress Severity = "SUPPRESS"
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

// severityRank orders severities from lowest to highest. Used for the
// --min-severity gate. SUPPRESS is always the minimum and can never be
// selected as a threshold — the CLI rejects it at parse time.
var severityRank = map[Severity]int{
	SeveritySuppress: 0,
	SeverityLow:      1,
	SeverityMedium:   2,
	SeverityHigh:     3,
	SeverityCritical: 4,
}

// Rank returns the ordinal rank of s. Unknown severities rank as -1 so
// callers can detect them without a separate lookup.
func Rank(s Severity) int {
	r, ok := severityRank[s]
	if !ok {
		return -1
	}
	return r
}

// ParseSeverity parses a user-facing severity string (case-insensitive)
// into a Severity constant. Returns the zero value and false on unknown
// input.
func ParseSeverity(s string) (Severity, bool) {
	switch upper(s) {
	case "SUPPRESS":
		return SeveritySuppress, true
	case "LOW":
		return SeverityLow, true
	case "MEDIUM":
		return SeverityMedium, true
	case "HIGH":
		return SeverityHigh, true
	case "CRITICAL":
		return SeverityCritical, true
	}
	return "", false
}

// upper is a tiny helper that avoids pulling strings for one call site
// in ParseSeverity — keeps this file dependency-free.
func upper(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		out[i] = c
	}
	return string(out)
}

// Score is the deterministic output of the scoring engine for one
// finding. Severity is the bucketed publish decision; Confidence and
// Impact are exposed for consumers that want to render or sort them.
// Rationale names the policy key that produced this score so users can
// trace why a finding was rated the way it was.
type Score struct {
	Severity   Severity `json:"severity"`
	Confidence float64  `json:"confidence"`
	Impact     float64  `json:"impact"`
	Rationale  string   `json:"rationale,omitempty"`
}

// ScoredComment pairs an LlmComment with its Score. The comment payload
// is left untouched; downstream emit adds the score fields to the JSON
// envelope so consumers can filter or render on severity/confidence.
type ScoredComment struct {
	model.LlmComment
	Score Score `json:"score"`
}
