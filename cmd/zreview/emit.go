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
// carried). Downstream posters read `state` to decide whether to create a
// new PR comment or update an existing one (Phase 17 concern).
type emittedComment struct {
	model.LlmComment
	State findings.State `json:"state,omitempty"`
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
		wrapped = append(wrapped, emittedComment{LlmComment: c, State: cfg.commentState(c)})
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
// severity, mirroring the behaviour described in docs/security.html.
func exitCodeForComments(comments []model.LlmComment) int {
	for _, c := range comments {
		switch c.Severity {
		case "critical", "blocker":
			return 3
		}
	}
	return 0
}
