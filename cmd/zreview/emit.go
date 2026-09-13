// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shubam-disseqt/z-code-reviewer/internal/findings"
	"github.com/shubam-disseqt/z-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
	"github.com/shubam-disseqt/z-code-reviewer/internal/overlap"
	"github.com/shubam-disseqt/z-code-reviewer/internal/scoring"
)

// Format identifies an emit target. Kept as strings to line up with the
// --format flag surface.
const (
	formatStdout = "stdout"
	formatJSON   = "json"
	formatGithub = "github"
)

// validFormat reports whether f is a supported --format value.
func validFormat(f string) bool {
	switch f {
	case formatStdout, formatJSON, formatGithub:
		return true
	}
	return false
}

// emitResult is the JSON output shape when --format=json.
type emitResult struct {
	SessionID string            `json:"session_id"`
	Comments  []emittedComment  `json:"comments"`
	Overlap   []overlap.Finding `json:"overlap,omitempty"`
}

// emittedComment wraps LlmComment with the reconciled state (new, keep,
// carried) and the Phase 16 scoring output. Downstream posters read
// `state` to decide whether to create a new PR comment or update an
// existing one (Phase 17 concern). The scoring fields override the raw
// LlmComment.Severity in the JSON envelope so consumers see the
// policy-bucketed decision, not the producer's raw label.
type emittedComment struct {
	model.LlmComment
	State findings.State `json:"state,omitempty"`

	// Score exposes the deterministic scoring engine output. Omitted
	// from the JSON envelope when the scorer wasn't invoked (severity
	// == ""), which keeps existing test fixtures stable.
	Severity   scoring.Severity `json:"severity,omitempty"`
	Confidence float64          `json:"confidence,omitempty"`
	Impact     float64          `json:"impact,omitempty"`
	Rationale  string           `json:"rationale,omitempty"`
}

// emit writes the comments in the chosen format. For "github" it posts each
// comment to the given PR and prints a summary to stdout.
func emit(ctx context.Context, cfg emitConfig) error {
	switch cfg.Format {
	case formatStdout:
		return emitStdout(cfg.Stdout, cfg.Comments, cfg.Overlap)
	case formatJSON:
		return emitJSON(cfg)
	case formatGithub:
		return emitGithub(ctx, cfg)
	default:
		return fmt.Errorf("emit: unsupported format %q", cfg.Format)
	}
}

// emitConfig bundles the emit inputs — smaller signatures beat a long arg list.
type emitConfig struct {
	Format    string
	Output    string
	SessionID string
	Comments  []model.LlmComment
	Overlap   []overlap.Finding

	// FindingState maps a comment's fingerprint to its reconciled state
	// (new, keep, carried). Empty when carry-over is disabled (--pr unset).
	FindingState map[string]findings.State

	// Scores maps commentKey(c) to the deterministic scoring output.
	// Only populated for JSON emit in v1 — stdout / github stay compact.
	Scores map[string]scoring.Score

	// GitHub-specific:
	GHClient  *gh.Client
	Owner     string
	Repo      string
	PRNumber  int
	CommitSHA string

	Stdout io.Writer
}

// commentState returns the reconciled state for a comment, or "" if none is
// known. Uses the same fingerprint function the carry-over stage used.
func (cfg *emitConfig) commentState(c model.LlmComment) findings.State {
	if len(cfg.FindingState) == 0 {
		return ""
	}
	return cfg.FindingState[commentFingerprint(cfg.Owner, cfg.Repo, c)]
}

// commentKey returns a stable identity string used to zip scoring results
// back to their originating comment during emit. Path + line-range + first
// 64 chars of content is enough to disambiguate within a single review
// run without pulling in a full fingerprint hash.
func commentKey(c model.LlmComment) string {
	content := c.Content
	if len(content) > 64 {
		content = content[:64]
	}
	return fmt.Sprintf("%s:%d-%d:%s", c.Path, c.StartLine, c.EndLine, content)
}

// emitStdout prints a human-readable block per comment.
func emitStdout(w io.Writer, comments []model.LlmComment, findings []overlap.Finding) error {
	if len(comments) == 0 {
		fmt.Fprintln(w, "No review findings.")
	}
	for i, c := range comments {
		fmt.Fprintf(w, "--- Finding %d ---\n", i+1)
		fmt.Fprintf(w, "  path:     %s:%d-%d\n", c.Path, c.StartLine, c.EndLine)
		if c.Severity != "" {
			fmt.Fprintf(w, "  severity: %s\n", c.Severity)
		}
		if c.Category != "" {
			fmt.Fprintf(w, "  category: %s\n", c.Category)
		}
		fmt.Fprintf(w, "  %s\n", strings.TrimSpace(c.Content))
		if c.SuggestionCode != "" {
			fmt.Fprintln(w, "  suggestion:")
			for _, line := range strings.Split(c.SuggestionCode, "\n") {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
		fmt.Fprintln(w)
	}
	if md := overlap.Render(findings); md != "" {
		fmt.Fprintln(w, "--- Cross-PR overlap ---")
		fmt.Fprintln(w, md)
	}
	return nil
}

// emitJSON writes the structured envelope to Output (- means stdout).
func emitJSON(cfg emitConfig) error {
	wrapped := make([]emittedComment, 0, len(cfg.Comments))
	for _, c := range cfg.Comments {
		ec := emittedComment{LlmComment: c, State: cfg.commentState(c)}
		if sc, ok := cfg.Scores[commentKey(c)]; ok {
			ec.Severity = sc.Severity
			ec.Confidence = sc.Confidence
			ec.Impact = sc.Impact
			ec.Rationale = sc.Rationale
		}
		wrapped = append(wrapped, ec)
	}
	res := emitResult{
		SessionID: cfg.SessionID,
		Comments:  wrapped,
		Overlap:   cfg.Overlap,
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return fmt.Errorf("emit json: %w", err)
	}
	data = append(data, '\n')

	if cfg.Output == "" || cfg.Output == "-" {
		_, err := cfg.Stdout.Write(data)
		return err
	}
	if err := os.WriteFile(cfg.Output, data, 0o644); err != nil {
		return fmt.Errorf("emit json: write %s: %w", cfg.Output, err)
	}
	return nil
}

// emitGithub posts each comment to the PR and prints a summary. Comments
// without a resolved line are skipped (GitHub rejects them).
func emitGithub(ctx context.Context, cfg emitConfig) error {
	if cfg.GHClient == nil {
		return fmt.Errorf("emit github: no GITHUB_TOKEN configured")
	}
	if cfg.PRNumber == 0 || cfg.Owner == "" || cfg.Repo == "" {
		return fmt.Errorf("emit github: --pr, owner, and repo are required")
	}
	var posted, skipped int
	for _, c := range cfg.Comments {
		if c.StartLine == 0 && c.EndLine == 0 {
			skipped++
			continue
		}
		line := c.EndLine
		if line == 0 {
			line = c.StartLine
		}
		rc := gh.ReviewComment{
			Path:      c.Path,
			Body:      formatGithubBody(c),
			Line:      line,
			StartLine: c.StartLine,
			CommitSHA: cfg.CommitSHA,
		}
		if err := cfg.GHClient.PostReviewComment(ctx, cfg.Owner, cfg.Repo, cfg.PRNumber, rc); err != nil {
			return fmt.Errorf("emit github: post to %s:%d: %w", c.Path, line, err)
		}
		posted++
	}
	fmt.Fprintf(cfg.Stdout, "Posted %d comment(s) to %s/%s#%d (skipped %d unresolved).\n",
		posted, cfg.Owner, cfg.Repo, cfg.PRNumber, skipped)
	return nil
}

// formatGithubBody wraps content, category and suggestion for GitHub's
// inline comment surface.
func formatGithubBody(c model.LlmComment) string {
	var b strings.Builder
	if c.Severity != "" || c.Category != "" {
		fmt.Fprintf(&b, "**[%s / %s]** ", c.Severity, c.Category)
	}
	b.WriteString(strings.TrimSpace(c.Content))
	if c.SuggestionCode != "" {
		b.WriteString("\n\n```suggestion\n")
		b.WriteString(c.SuggestionCode)
		if !strings.HasSuffix(c.SuggestionCode, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("```")
	}
	return b.String()
}

// exitCodeForComments returns 3 when any comment carries a critical/blocker
// severity, mirroring the behaviour described in docs/security.html. When
// scores are provided, the scoring-engine severity (authoritative) wins
// over the raw LlmComment.Severity; otherwise the raw label is used.
func exitCodeForComments(comments []model.LlmComment, scores map[string]scoring.Score) int {
	for _, c := range comments {
		if sc, ok := scores[commentKey(c)]; ok {
			if sc.Severity == scoring.SeverityCritical {
				return 3
			}
			continue
		}
		switch c.Severity {
		case "critical", "blocker":
			return 3
		}
	}
	return 0
}
