// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/core/overlap.py
// under Apache License 2.0.

// Package overlap flags other open PRs that step on the one under review —
// either editing the same code (merge-conflict risk) or pursuing the same
// goal (duplicate effort). The pipeline is deterministic pre-filter first,
// one batched LLM verdict second; failures are swallowed so the review never
// blocks on overlap detection.
package overlap

// Config tunes the pipeline. DefaultConfig matches the Mira reference.
type Config struct {
	Enabled                  bool
	MaxCandidates            int
	ConfidenceFloor          float64
	TitleSimilarityThreshold float64
}

// DefaultConfig returns Mira's default overlap settings.
func DefaultConfig() Config {
	return Config{
		Enabled:                  true,
		MaxCandidates:            20,
		ConfidenceFloor:          0.6,
		TitleSimilarityThreshold: 0.4,
	}
}

// Finding is what Render turns into the walkthrough block.
type Finding struct {
	Number      int
	HTMLURL     string
	Title       string
	Kind        string // "merge_conflict" | "duplicate_effort" | "both"
	Confidence  float64
	Reason      string
	SharedFiles []string
}

// PR describes the PR under review. Paths come from the diff; Symbols are
// optional (empty is fine — the pre-filter will just fall back to file/title
// intersection).
type PR struct {
	Owner, Repo string
	Number      int
	Title, Body string
	HeadRef     string
	BaseRef     string
	HeadSHA     string
	UserLogin   string
	Paths       []string
	Symbols     []string
}

// fingerprint is the internal projection of a candidate PR into the shape
// the pre-filter compares against. Mira caches these per PR keyed on
// head_sha; this session doesn't persist them (see overlap.go).
type fingerprint struct {
	Number  int
	HeadSHA string
	Title   string
	Body    string
	Paths   []string
	Symbols []string
}

// kindRank orders findings — higher renders first.
var kindRank = map[string]int{
	"both":             3,
	"merge_conflict":   2,
	"duplicate_effort": 1,
	"none":             0,
}
