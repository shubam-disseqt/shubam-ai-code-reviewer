// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package effort computes a deterministic 0-10 reviewer-effort score from
// a PR's diff, findings, and cross-PR context. The score is reproducible
// (same inputs → same output), auditable (every contribution is named), and
// configurable via YAML policy — mirroring the scoring engine's shape.
package effort

// Score is the reviewer-effort output. Value is clamped to [0, 10] and
// rounded to the nearest integer for display. Label is derived from Value.
// Contributions list every signal that fed the score so operators can
// audit "why is this a 7?".
type Score struct {
	Value         int            `json:"value"`
	Label         string         `json:"label"`
	Dot           string         `json:"dot"` // 🟢 / 🟡 / 🔴
	Raw           float64        `json:"raw"` // pre-round, pre-clamp
	Contributions []Contribution `json:"contributions"`
}

// Contribution is one signal → number entry rendered in the description
// block's audit table. Cap is set when a per-signal ceiling was hit so the
// operator sees why more churn didn't push the score higher.
type Contribution struct {
	Signal string  `json:"signal"`
	Detail string  `json:"detail,omitempty"`
	Points float64 `json:"points"`
	Capped bool    `json:"capped,omitempty"`
}

// Inputs bundles everything Compute needs from the review pipeline. Kept
// as one struct so the call site (cmd/zreview/review_cmd.go) doesn't grow
// a 10-arg function signature.
type Inputs struct {
	Files          []FileDelta
	FindingsBySev  map[string]int // "CRITICAL"|"HIGH"|"MEDIUM"|"LOW" → count
	OverlappingPRs int
}

// FileDelta is a per-file summary derived from model.Diff. LinesAdded and
// LinesDeleted are counted from the raw unified-diff body — the caller
// splits, this package only reads.
type FileDelta struct {
	Path         string
	LinesAdded   int
	LinesDeleted int
	IsNew        bool
	IsDeleted    bool
	IsRenamed    bool
	// IsTest reports whether the path matches the test-file heuristic. The
	// caller could compute this too, but keeping it here means one owner
	// for the file-classification rules.
	IsTest bool
}
