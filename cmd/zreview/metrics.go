// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"fmt"
	"log/slog"

	"github.com/shubam-disseqt/z-code-reviewer/internal/logutil"
)

// Metrics is the per-review aggregate scoreboard emitted once at the end of
// runReview. Populated as the pipeline progresses; empty fields survive as
// zero. The shape stays deliberately flat so JSON consumers can drop it
// straight into a metrics store without walking a tree.
//
// Cost is derived from token counters, not from provider billing responses —
// the LLM client doesn't surface per-request USD and hardcoding provider
// prices here would go stale. TotalCostUSD stays zero unless a caller sets
// it explicitly; downstream can multiply tokens by their own rate card.
// ponytail: cost=0.0 until a provider price table lands, hook it in Emit.
type Metrics struct {
	DurationMs       int64   `json:"duration_ms"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	FilesReviewed    int     `json:"files_reviewed"`
	CommentsPosted   int     `json:"comments_posted"`
	ScannerFindings  int     `json:"scanner_findings"`
	CarriedFindings  int     `json:"carried_findings"`
	ResolvedFindings int     `json:"resolved_findings"`
	NewFindings      int     `json:"new_findings"`
}

// emitMetrics writes a single INFO record with stage="metrics" so it stands
// out in the log stream. In text mode the message is a compact greppable
// summary; in JSON mode every field ships as a structured attr so the record
// slots into an observability pipeline directly.
func emitMetrics(logger *slog.Logger, m Metrics) {
	if logger == nil {
		return
	}
	// Compact text summary; keys chosen to match the JSON attr names so
	// scripts written against one shape can reuse patterns for the other.
	msg := fmt.Sprintf(
		"duration=%s files=%d tokens=in:%d/out:%d cost=$%.4f findings=new:%d/carried:%d/resolved:%d scanner=%d comments=%d",
		formatDurationMs(m.DurationMs),
		m.FilesReviewed,
		m.PromptTokens,
		m.CompletionTokens,
		m.TotalCostUSD,
		m.NewFindings,
		m.CarriedFindings,
		m.ResolvedFindings,
		m.ScannerFindings,
		m.CommentsPosted,
	)
	logutil.WithStage(logger, "metrics").Info(
		msg,
		"duration_ms", m.DurationMs,
		"files_reviewed", m.FilesReviewed,
		"prompt_tokens", m.PromptTokens,
		"completion_tokens", m.CompletionTokens,
		"total_cost_usd", m.TotalCostUSD,
		"new_findings", m.NewFindings,
		"carried_findings", m.CarriedFindings,
		"resolved_findings", m.ResolvedFindings,
		"scanner_findings", m.ScannerFindings,
		"comments_posted", m.CommentsPosted,
	)
}

// formatDurationMs renders milliseconds as "1.2s" / "42ms" — mirrors what
// operators expect from a CLI summary. Kept private to metrics.go.
func formatDurationMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000.0)
}
