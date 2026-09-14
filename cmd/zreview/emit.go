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
	"time"

	"github.com/shubam-disseqt/z-code-reviewer/internal/effort"
	"github.com/shubam-disseqt/z-code-reviewer/internal/findings"
	"github.com/shubam-disseqt/z-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
	"github.com/shubam-disseqt/z-code-reviewer/internal/overlap"
	"github.com/shubam-disseqt/z-code-reviewer/internal/sarif"
	"github.com/shubam-disseqt/z-code-reviewer/internal/scoring"
)

// Format identifies an emit target. Kept as strings to line up with the
// --format flag surface.
const (
	formatStdout = "stdout"
	formatJSON   = "json"
	formatGithub = "github"
	formatSARIF  = "sarif"
)

// validFormat reports whether f is a supported --format value.
func validFormat(f string) bool {
	switch f {
	case formatStdout, formatJSON, formatGithub, formatSARIF:
		return true
	}
	return false
}

// emitResult is the JSON output shape when --format=json.
type emitResult struct {
	SessionID string            `json:"session_id"`
	Comments  []emittedComment  `json:"comments"`
	Overlap   []overlap.Finding `json:"overlap,omitempty"`
	Summary   *model.Summary    `json:"summary,omitempty"`
	Labels    *model.Labels     `json:"labels,omitempty"`
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
	case formatSARIF:
		return emitSARIF(cfg)
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

	// Summary and Labels come from the Phase 15 cheap-tier calls. Only the
	// JSON emitter surfaces them today; stdout/github ignore them until
	// Phase 17 consumes them for the PR description block.
	Summary model.Summary
	Labels  model.Labels

	// Scores maps commentKey(c) to the deterministic scoring output.
	// Only populated for JSON emit in v1 — stdout / github stay compact.
	Scores map[string]scoring.Score

	// Effort is the reviewer-effort score (0-10 + audit trail) that ends
	// up above the severity table in the PR description block.
	Effort effort.Score

	// GitHub-specific:
	GHClient  *gh.Client
	Owner     string
	Repo      string
	PRNumber  int
	CommitSHA string
	// Ref is the git ref for SARIF uploads (e.g. "refs/pull/42/head").
	// Empty falls back to refs/heads/main inside the SARIF upload path.
	Ref string
	// WaitSARIF blocks after upload until GitHub reports the SARIF as
	// "complete" or "failed". Off by default; the CLI --wait-sarif flag
	// toggles it.
	WaitSARIF bool

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
	if !isZeroSummary(cfg.Summary) {
		s := cfg.Summary
		res.Summary = &s
	}
	if !isZeroLabels(cfg.Labels) {
		l := cfg.Labels
		res.Labels = &l
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
// without a resolved line are skipped (GitHub rejects them). Scanner-sourced
// comments are skipped for inline posting — they go to SARIF instead — so
// PRs don't drown in high-precision but low-context findings on the diff.
// The PR description block (Phase 17) is updated after posting.
func emitGithub(ctx context.Context, cfg emitConfig) error {
	if cfg.GHClient == nil {
		return fmt.Errorf("emit github: no GITHUB_TOKEN configured")
	}
	if cfg.PRNumber == 0 || cfg.Owner == "" || cfg.Repo == "" {
		return fmt.Errorf("emit github: --pr, owner, and repo are required")
	}
	var skipped, scannerCount int
	batch := make([]gh.ReviewComment, 0, len(cfg.Comments))
	// freshFPs tracks fingerprints we're about to post so the cleanup can
	// tell "still valid" from "stale".
	freshFPs := make(map[string]struct{}, len(cfg.Comments))
	for _, c := range cfg.Comments {
		if isScannerSource(c.Source) {
			scannerCount++
			continue
		}
		if c.StartLine == 0 && c.EndLine == 0 {
			skipped++
			continue
		}
		line := c.EndLine
		if line == 0 {
			line = c.StartLine
		}
		fp := commentFingerprint(cfg.Owner, cfg.Repo, c)
		freshFPs[fp] = struct{}{}
		batch = append(batch, gh.ReviewComment{
			Path:      c.Path,
			Body:      formatGithubBodyWithFP(c, fp),
			Line:      line,
			StartLine: c.StartLine,
			CommitSHA: cfg.CommitSHA,
		})
	}
	// Delete stale zreview-authored comments before posting the new batch.
	// A comment is stale when its fingerprint marker is NOT in freshFPs —
	// meaning the finding was resolved (fixed, or code moved) since the
	// last review run.
	//
	// Legacy: comments posted before zreview learned to embed a
	// fingerprint marker are identified by the "**[SEV / category]** "
	// prefix pattern our own formatter uses. They have no marker to
	// reconcile against, so we treat them all as stale — the fresh batch
	// posted this run becomes the new source of truth.
	//
	// Non-zreview comments (human reviewers, other bots) have neither the
	// marker nor the prefix and are left alone.
	deleted := 0
	if existing, err := cfg.GHClient.ListReviewComments(ctx, cfg.Owner, cfg.Repo, cfg.PRNumber); err == nil {
		for _, e := range existing {
			fp := extractZreviewFingerprint(e.Body)
			switch {
			case fp != "":
				if _, keep := freshFPs[fp]; keep {
					continue // still-valid finding
				}
			case looksLikeLegacyZreviewComment(e.Body):
				// pre-marker zreview comment — delete unconditionally
			default:
				continue // human or other bot
			}
			if err := cfg.GHClient.DeleteReviewComment(ctx, cfg.Owner, cfg.Repo, e.ID); err == nil {
				deleted++
			}
		}
	}
	// Batch inline comments into review submissions of up to 20 each.
	// One-shot submission is preferable (avoids the per-comment 422
	// "submitted too quickly" secondary rate limit) but GitHub's review
	// endpoint drops payloads above ~30-40 comments with a stream reset.
	// 20 is a conservative ceiling that keeps the wall time low on large
	// PRs while staying inside the endpoint's soft limits.
	const reviewBatchSize = 20
	for i := 0; i < len(batch); i += reviewBatchSize {
		end := i + reviewBatchSize
		if end > len(batch) {
			end = len(batch)
		}
		chunk := batch[i:end]
		summary := fmt.Sprintf("zreview review (%d–%d of %d)", i+1, end, len(batch))
		if err := cfg.GHClient.PostReview(ctx, cfg.Owner, cfg.Repo, cfg.PRNumber, cfg.CommitSHA, summary, chunk); err != nil {
			return fmt.Errorf("emit github: post review batch %d-%d: %w", i+1, end, err)
		}
		// Small pause between batches — belt-and-braces against the
		// secondary rate limit on rapid review submissions.
		if end < len(batch) {
			time.Sleep(500 * time.Millisecond)
		}
	}
	fmt.Fprintf(cfg.Stdout, "Posted %d comment(s) to %s/%s#%d (skipped %d unresolved, %d scanner→SARIF, deleted %d stale).\n",
		len(batch), cfg.Owner, cfg.Repo, cfg.PRNumber, skipped, scannerCount, deleted)

	// Best-effort SARIF upload when the operator has opted in.
	if scannerCount > 0 && os.Getenv("ZREVIEW_UPLOAD_SARIF") == "1" {
		if err := uploadScannerSARIF(ctx, cfg); err != nil {
			fmt.Fprintf(cfg.Stdout, "emit github: SARIF upload failed: %v (continuing)\n", err)
		}
	}

	// PR description block — best-effort; a failure logs and lets the
	// review exit succeed. Overlap findings are threaded in so the "PR#2
	// collides with this one" warning surfaces where the author will see
	// it, not just in the standalone `zreview overlap` command's stdout.
	counts := scoreCountsFromMap(cfg.Scores)
	scannerComments := make([]model.LlmComment, 0)
	for _, c := range cfg.Comments {
		if isScannerSource(c.Source) {
			scannerComments = append(scannerComments, c)
		}
	}
	if err := UpdateDescription(ctx, cfg.GHClient, cfg.Owner, cfg.Repo, cfg.PRNumber, cfg.Summary, cfg.Labels, counts, cfg.Overlap, scannerComments, cfg.Effort); err != nil {
		fmt.Fprintf(cfg.Stdout, "emit github: description update failed: %v (continuing)\n", err)
	}
	return nil
}

// isScannerSource returns true for LlmComment.Source values Phase 14 tags
// on scanner-derived comments — "scanner:gitleaks", "scanner:semgrep",
// "scanner:govulncheck", etc.
func isScannerSource(source string) bool {
	return strings.HasPrefix(source, "scanner:")
}

// uploadScannerSARIF filters scanner-sourced comments, builds sarif.Finding
// entries, encodes the log, and hands it to gh.UploadSARIF. Ref falls back
// to refs/heads/main when the caller didn't provide one — Code Scanning
// requires the ref to attach the analysis. When cfg.WaitSARIF is set, we
// poll GitHub for the terminal processing status and log it; a "failed"
// terminal returns an error so the caller can surface the outcome.
func uploadScannerSARIF(ctx context.Context, cfg emitConfig) error {
	findings := scannerFindingsForSARIF(cfg)
	if len(findings) == 0 {
		return nil
	}
	blob, err := sarif.Encode(findings, sarif.Meta{
		Repo:    cfg.Owner + "/" + cfg.Repo,
		HeadSHA: cfg.CommitSHA,
		Version: Version,
	})
	if err != nil {
		return err
	}
	if err := sarif.Validate(blob); err != nil {
		return err
	}
	ref := cfg.Ref
	if ref == "" {
		ref = "refs/heads/main"
	}
	id, err := cfg.GHClient.UploadSARIF(ctx, cfg.Owner, cfg.Repo, cfg.CommitSHA, ref, blob)
	if err != nil {
		return err
	}
	if !cfg.WaitSARIF || id == "" {
		return nil
	}
	// Cap wait at 5 minutes — GitHub typically finishes in under a minute
	// but pathological uploads can stall. Beyond that, treat it as an
	// operator problem and stop blocking the review's exit.
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	status, err := cfg.GHClient.WaitSARIF(waitCtx, cfg.Owner, cfg.Repo, id)
	if err != nil {
		return fmt.Errorf("wait sarif %s: %w", id, err)
	}
	fmt.Fprintf(cfg.Stdout, "emit github: SARIF %s → %s\n", id, status)
	if status == "failed" {
		return fmt.Errorf("SARIF %s: processing_status=failed", id)
	}
	return nil
}

// scannerFindingsForSARIF projects scanner-sourced comments into
// sarif.Finding. Non-scanner comments are skipped so a shared review does
// not surface LLM-derived stylistic comments as Code Scanning alerts.
func scannerFindingsForSARIF(cfg emitConfig) []sarif.Finding {
	out := make([]sarif.Finding, 0, len(cfg.Comments))
	for _, c := range cfg.Comments {
		if !isScannerSource(c.Source) {
			continue
		}
		f := sarif.Finding{
			Source:      c.Source,
			RuleID:      c.Category, // scanner adapters map RuleID → Category="security"; fall through to Content
			Description: strings.TrimSpace(c.Content),
			Path:        c.Path,
			StartLine:   c.StartLine,
			EndLine:     c.EndLine,
		}
		// Prefer the scoring-engine severity when present; fall back to the
		// raw label so the SARIF level is never blank.
		if sc, ok := cfg.Scores[commentKey(c)]; ok {
			f.Severity = sc.Severity
		} else {
			f.Severity = severityFromRawLabel(c.Severity)
		}
		if cfg.FindingState != nil {
			f.Fingerprint = commentFingerprint(cfg.Owner, cfg.Repo, c)
		}
		out = append(out, f)
	}
	return out
}

// severityFromRawLabel maps the pre-scoring LlmComment.Severity string onto
// scoring.Severity for the SARIF level. Unknown values fall back to
// MEDIUM — matches the SARIF encoder's own "unknown → warning" default.
func severityFromRawLabel(raw string) scoring.Severity {
	if s, ok := scoring.ParseSeverity(raw); ok {
		return s
	}
	return scoring.SeverityMedium
}

// emitSARIF filters scanner-sourced comments, encodes them as SARIF 2.1.0,
// and writes to cfg.Output (file or stdout via "-"). LLM-derived comments
// do not appear in SARIF — the format is reserved for deterministic
// tool findings that GitHub Code Scanning knows how to render.
func emitSARIF(cfg emitConfig) error {
	findings := scannerFindingsForSARIF(cfg)
	blob, err := sarif.Encode(findings, sarif.Meta{
		Repo:    cfg.Owner + "/" + cfg.Repo,
		HeadSHA: cfg.CommitSHA,
		Version: Version,
	})
	if err != nil {
		return fmt.Errorf("emit sarif: %w", err)
	}
	if cfg.Output == "" || cfg.Output == "-" {
		_, err := cfg.Stdout.Write(blob)
		return err
	}
	if err := os.WriteFile(cfg.Output, blob, 0o644); err != nil {
		return fmt.Errorf("emit sarif: write %s: %w", cfg.Output, err)
	}
	return nil
}

// formatGithubBody wraps content, category and suggestion for GitHub's
// inline comment surface.
func formatGithubBody(c model.LlmComment) string {
	return formatGithubBodyWithFP(c, "")
}

// zreviewFPMarkerPrefix is the HTML-comment marker prefix that zreview
// injects into every posted comment body so the NEXT review run can find
// its own past comments and delete the stale ones. Kept as a distinctive
// constant so operators can grep for it and downstream tooling can find
// zreview-authored comments without a bot-user lookup.
const zreviewFPMarkerPrefix = "<!-- zreview:fp:"

// formatGithubBodyWithFP appends a hidden fingerprint marker used by the
// stale-comment cleanup on subsequent runs. Empty fp skips the marker.
func formatGithubBodyWithFP(c model.LlmComment, fp string) string {
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
	if fp != "" {
		fmt.Fprintf(&b, "\n\n%s%s -->", zreviewFPMarkerPrefix, fp)
	}
	return b.String()
}

// looksLikeLegacyZreviewComment matches the prefix zreview's formatter
// emits ("**[<severity> / <category>]** ") without a fingerprint marker.
// Used to clean up pre-marker comments from earlier releases.
func looksLikeLegacyZreviewComment(body string) bool {
	if strings.Contains(body, zreviewFPMarkerPrefix) {
		return false
	}
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "**[") {
		return false
	}
	// Look for "**[X / Y]** " where the closing "]** " lives within the
	// first ~80 chars. Anything wider is unlikely to be our formatter.
	close := strings.Index(trimmed, "]** ")
	if close < 0 || close > 80 {
		return false
	}
	return strings.Contains(trimmed[:close], " / ")
}

// extractZreviewFingerprint pulls the fingerprint hex from an existing
// comment body, or "" if the comment isn't zreview-authored.
func extractZreviewFingerprint(body string) string {
	i := strings.Index(body, zreviewFPMarkerPrefix)
	if i < 0 {
		return ""
	}
	rest := body[i+len(zreviewFPMarkerPrefix):]
	j := strings.Index(rest, " -->")
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// isZeroSummary reports whether a Summary carries no signal — used so the
// JSON emitter omits an all-empty Phase 15 payload instead of writing
// `"summary": {}`.
func isZeroSummary(s model.Summary) bool {
	return s.Walkthrough == "" && len(s.ChangeGroups) == 0 && s.TestingNotes == "" && s.Risk == ""
}

// isZeroLabels mirrors isZeroSummary for the labeler payload.
func isZeroLabels(l model.Labels) bool {
	return l.PRType == "" && len(l.Domains) == 0 && l.RiskTag == "" && len(l.OwnershipHints) == 0
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
