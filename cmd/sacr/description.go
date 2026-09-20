// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/effort"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/overlap"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/scoring"
)

// summaryFingerprint marks sacr-authored summary comments so future runs
// can find and replace their previous post without touching human comments.
const summaryFingerprint = "<!-- sacr:fp:summary -->"

// summaryClient is the narrow interface PostSummaryReview needs — kept
// local so tests can supply a fake without touching the real HTTP layer.
type summaryClient interface {
	PostIssueComment(ctx context.Context, owner, repo string, number int, body string) (gh.IssueComment, error)
	ListIssueComments(ctx context.Context, owner, repo string, number int) ([]gh.IssueComment, error)
	DeleteIssueComment(ctx context.Context, owner, repo string, commentID int64) error
}

// labelClient is the narrow interface ApplyLabels needs.
type labelClient interface {
	AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error
	ListLabels(ctx context.Context, owner, repo string, number int) ([]string, error)
	RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error
}

// PostSummaryReview posts the sacr review summary as a PR-level issue
// comment authored by github-actions[bot]. Previous sacr summary comments
// (identified by the fingerprint marker) are deleted first — best-effort,
// non-fatal — so re-runs replace rather than accumulate.
func PostSummaryReview(
	ctx context.Context,
	client summaryClient,
	owner, repo string, pr int,
	summary model.Summary,
	labels model.Labels,
	scoreCounts map[scoring.Severity]int,
	overlapFindings []overlap.Finding,
	scannerFindings []model.LlmComment,
	effortScore effort.Score,
	pkgDiagram string,
) error {
	if client == nil {
		return fmt.Errorf("post summary: nil client")
	}
	if owner == "" || repo == "" || pr == 0 {
		return fmt.Errorf("post summary: owner/repo/pr required")
	}

	body := RenderSummary(summary, labels, scoreCounts, overlapFindings, scannerFindings, effortScore, pkgDiagram) + "\n\n" + summaryFingerprint

	// Best-effort cleanup of prior sacr summary comments; individual delete
	// failures are logged-ish (swallowed) so a stale comment doesn't block
	// the fresh post.
	if existing, err := client.ListIssueComments(ctx, owner, repo, pr); err == nil {
		for _, c := range existing {
			if strings.Contains(c.Body, summaryFingerprint) {
				_ = client.DeleteIssueComment(ctx, owner, repo, c.ID)
			}
		}
	}

	if _, err := client.PostIssueComment(ctx, owner, repo, pr, body); err != nil {
		return fmt.Errorf("post summary: %w", err)
	}
	return nil
}

// ApplyLabels escalates risk from findings, drops stale exclusive labels,
// and applies the fresh set. Idempotent and best-effort on the list/remove
// steps — a missing list API doesn't stop the add.
func ApplyLabels(
	ctx context.Context,
	client labelClient,
	owner, repo string, pr int,
	labels model.Labels,
	scoreCounts map[scoring.Severity]int,
	scannerFindings []model.LlmComment,
) error {
	if client == nil {
		return fmt.Errorf("apply labels: nil client")
	}
	if owner == "" || repo == "" || pr == 0 {
		return fmt.Errorf("apply labels: owner/repo/pr required")
	}

	labels = escalateRiskFromFindings(labels, scoreCounts, scannerFindings)
	lbls := labelSet(labels)
	if len(lbls) == 0 {
		return nil
	}
	if existing, err := client.ListLabels(ctx, owner, repo, pr); err == nil {
		for _, drop := range staleExclusiveLabels(existing, lbls) {
			_ = client.RemoveLabel(ctx, owner, repo, pr, drop)
		}
	}
	if err := client.AddLabels(ctx, owner, repo, pr, lbls); err != nil {
		return fmt.Errorf("apply labels: %w", err)
	}
	return nil
}

// staleExclusiveLabels returns the subset of `existing` that belongs to an
// exclusive namespace and is NOT in the fresh set. Exclusive namespaces:
//   - risk/* (labeler emits exactly one)
//   - pr_type (labeler emits exactly one of feat|fix|refactor|docs|test|
//     chore|perf|ci|security). Only cleaned when the fresh set contains
//     a pr_type value, so a labeler that emits nothing doesn't clobber
//     human-applied labels like "test" on a code-only PR.
//
// Non-exclusive labels (domains, ownership hints) are additive and left
// alone.
func staleExclusiveLabels(existing, fresh []string) []string {
	freshSet := make(map[string]struct{}, len(fresh))
	for _, f := range fresh {
		freshSet[strings.ToLower(f)] = struct{}{}
	}
	freshHasPRType := false
	for f := range freshSet {
		if _, ok := prTypeLabels[f]; ok {
			freshHasPRType = true
			break
		}
	}
	var drop []string
	for _, e := range existing {
		lower := strings.ToLower(e)
		isRisk := strings.HasPrefix(lower, "risk/")
		_, isPRType := prTypeLabels[lower]
		exclusive := isRisk || (freshHasPRType && isPRType)
		if !exclusive {
			continue
		}
		if _, keep := freshSet[lower]; keep {
			continue
		}
		drop = append(drop, e)
	}
	return drop
}

// prTypeLabels enumerates the pr_type enum from the labeler prompt.
// Keep in sync with internal/prompts/labeler.md.
var prTypeLabels = map[string]struct{}{
	"feat":     {},
	"fix":      {},
	"refactor": {},
	"docs":     {},
	"test":     {},
	"chore":    {},
	"perf":     {},
	"ci":       {},
	"security": {},
}

// RenderSummary produces the markdown block sacr publishes with each
// review. Pure — no IO. Used by PostSummaryReview.
func RenderSummary(
	summary model.Summary,
	labels model.Labels,
	counts map[scoring.Severity]int,
	overlapFindings []overlap.Finding,
	scannerFindings []model.LlmComment,
	effortScore effort.Score,
	pkgDiagram string,
) string {
	var b strings.Builder
	b.WriteString("## Automated review by sacr\n\n")

	// Reviewer effort — top of the block. It's the single-number "how
	// much work is this to review?" summary the author sees before any
	// wall of finding text.
	if md := renderEffort(effortScore); md != "" {
		b.WriteString(md)
		b.WriteString("\n")
	}

	if summary.Walkthrough != "" {
		b.WriteString(strings.TrimSpace(summary.Walkthrough))
		b.WriteString("\n\n")
	}

	// Package import diagram — DETERMINISTIC. Parsed from the changed
	// files' actual `import` lines by internal/depgraph, not the LLM. If
	// the diff has no cross-package Go imports the section is omitted
	// entirely. sanitizeMermaid strips edge-label `|` inside node labels
	// as a defensive belt-and-suspenders — depgraph output shouldn't
	// contain them but the sanitizer costs nothing.
	if d := sanitizeMermaid(pkgDiagram); d != "" {
		b.WriteString("### Package imports (parsed from source)\n\n```mermaid\n")
		b.WriteString(d)
		b.WriteString("\n```\n\n")
	}

	// Overlap goes above the severity table because a merge collision is
	// more actionable than a per-finding count — the author needs to know
	// to coordinate BEFORE spending time on the review comments.
	if md := overlap.Render(overlapFindings); md != "" {
		b.WriteString("### Potential overlap with other PRs\n\n")
		b.WriteString(strings.TrimSpace(md))
		b.WriteString("\n\n")
	}

	// Scanner findings are routed to SARIF for the inline-comment path
	// (they're noisy on repeat pushes) but SARIF upload requires GitHub
	// Advanced Security, which many repos don't have. If we don't also
	// list them here the severity table shows "HIGH: 1" with no way to
	// know WHERE. Render as a compact table so authors can act on them
	// even when SARIF isn't the delivery channel.
	if md := renderScannerFindings(scannerFindings); md != "" {
		b.WriteString("### Scanner findings\n\n")
		b.WriteString(md)
		b.WriteString("\n")
	}

	b.WriteString(renderSeverityTable(counts))

	if labels.RiskTag != "" {
		fmt.Fprintf(&b, "\n**Risk:** `%s`", labels.RiskTag)
		if summary.Risk != "" && summary.Risk != labels.RiskTag {
			fmt.Fprintf(&b, " — %s", strings.TrimSpace(summary.Risk))
		}
		b.WriteString("\n")
	} else if summary.Risk != "" {
		fmt.Fprintf(&b, "\n**Risk:** %s\n", strings.TrimSpace(summary.Risk))
	}

	if len(summary.ChangeGroups) > 0 {
		b.WriteString("\n### Change groups\n\n")
		for _, g := range summary.ChangeGroups {
			title := strings.TrimSpace(g.Title)
			if title == "" {
				title = "(untitled)"
			}
			fmt.Fprintf(&b, "- **%s**", title)
			if s := strings.TrimSpace(g.Summary); s != "" {
				fmt.Fprintf(&b, " — %s", s)
			}
			if len(g.Files) > 0 {
				fmt.Fprintf(&b, " (%s)", strings.Join(g.Files, ", "))
			}
			b.WriteByte('\n')
		}
	}

	if strings.TrimSpace(summary.TestingNotes) != "" {
		b.WriteString("\n### Testing notes\n\n")
		b.WriteString(strings.TrimSpace(summary.TestingNotes))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderEffort renders the reviewer-effort section: headline value + dot,
// then a compact table of every input contribution (audit trail). Empty
// Score returns "" so trivial or unpopulated runs omit the section.
func renderEffort(s effort.Score) string {
	if s.Value == 0 && len(s.Contributions) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Reviewer effort\n\n")
	fmt.Fprintf(&b, "**%s %d / 10 — %s**\n\n", s.Dot, s.Value, s.Label)
	b.WriteString("<details><summary>How this was calculated</summary>\n\n")
	b.WriteString("| Signal | Detail | Contribution |\n| --- | --- | --- |\n")
	for _, c := range s.Contributions {
		note := ""
		if c.Capped {
			note = " (cap)"
		}
		fmt.Fprintf(&b, "| %s | %s | %+.2f%s |\n", c.Signal, escapePipes(c.Detail), c.Points, note)
	}
	b.WriteString("\n_Reproducible: same diff → same score. Weights configurable via `.sacr/effort.yaml` or `$SACR_EFFORT_POLICY`._\n")
	b.WriteString("</details>\n")
	return b.String()
}

// escapePipes keeps a `|` inside a Markdown table cell from being read as
// a column break — same footgun as the Mermaid `|` fix.
func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// sanitizeMermaid returns a Mermaid flowchart body safe to inline inside a
// ```mermaid``` fence. Rules the summarizer's LLM output has been observed
// to violate:
//   - `|` inside `[label]` — Mermaid parses `|` as edge-label syntax and
//     rejects the whole diagram (README bug)
//   - literal `\n` escape sequences (backslash+n) rather than newlines
//   - accidental ```mermaid fence wrapper
//
// Returns "" when the input isn't a plausible flowchart body — better to
// drop the section than break the rest of the description block.
func sanitizeMermaid(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Strip an accidental fence wrapper if the LLM added one.
	s = strings.TrimPrefix(s, "```mermaid")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	// Real newlines only.
	s = strings.ReplaceAll(s, "\\n", "\n")
	if !strings.HasPrefix(s, "flowchart") && !strings.HasPrefix(s, "graph") {
		return ""
	}
	// Replace `|` inside brackets with `/`.
	var out strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			if depth > 0 {
				depth--
			}
		case '|':
			if depth > 0 {
				out.WriteRune('/')
				continue
			}
		}
		out.WriteRune(r)
	}
	return strings.TrimSpace(out.String())
}

// escalateRiskFromFindings raises Labels.RiskTag to reflect what the
// downstream evidence actually shows. Rules:
//   - any CRITICAL score → risk/critical
//   - any HIGH score OR any scanner finding of severity HIGH+ → risk/high
//   - otherwise the labeler's original tag is left alone
//
// Ordering (low < medium < high < critical) uses the string order below.
// The function never DE-escalates — if the labeler already said high but
// the evidence says low, keep the higher signal.
func escalateRiskFromFindings(l model.Labels, counts map[scoring.Severity]int, scanners []model.LlmComment) model.Labels {
	rank := map[string]int{
		"risk/low":      1,
		"risk/medium":   2,
		"risk/high":     3,
		"risk/critical": 4,
	}
	current := rank[strings.ToLower(strings.TrimSpace(l.RiskTag))]

	target := 0
	if counts[scoring.SeverityCritical] > 0 {
		target = rank["risk/critical"]
	} else if counts[scoring.SeverityHigh] > 0 {
		target = rank["risk/high"]
	}
	for _, c := range scanners {
		if !strings.HasPrefix(c.Source, "scanner:") {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(string(c.Severity))) {
		case "CRITICAL":
			if target < rank["risk/critical"] {
				target = rank["risk/critical"]
			}
		case "HIGH", "ERROR":
			if target < rank["risk/high"] {
				target = rank["risk/high"]
			}
		}
	}

	if target == 0 || target <= current {
		return l
	}
	for tag, r := range rank {
		if r == target {
			l.RiskTag = tag
			break
		}
	}
	return l
}

// renderScannerFindings prints a compact markdown table of the scanner
// findings (comments whose Source is `scanner:*`). Empty list returns "".
// Truncates rule and message text so the description block stays scannable
// even when a scanner emits many findings.
func renderScannerFindings(comments []model.LlmComment) string {
	rows := make([][4]string, 0, len(comments))
	for _, c := range comments {
		if !strings.HasPrefix(c.Source, "scanner:") {
			continue
		}
		tool := strings.TrimPrefix(c.Source, "scanner:")
		rule := c.Category
		if rule == "" {
			rule = "-"
		}
		msg := oneLine(c.Content)
		if len(msg) > 100 {
			msg = msg[:97] + "..."
		}
		loc := fmt.Sprintf("`%s:%d`", c.Path, c.StartLine)
		rows = append(rows, [4]string{tool, rule, loc, msg})
	}
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("| Tool | Rule | Location | Finding |\n| --- | --- | --- | --- |\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", r[0], r[1], r[2], r[3])
	}
	return b.String()
}

// oneLine collapses whitespace runs to single spaces so a multi-line
// scanner description renders inside a single markdown table cell.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// severityOrder is the display order for the severity table. SUPPRESS is
// excluded because a suppressed finding is dropped before it reaches emit.
var severityOrder = []scoring.Severity{
	scoring.SeverityCritical,
	scoring.SeverityHigh,
	scoring.SeverityMedium,
	scoring.SeverityLow,
}

// renderSeverityTable prints a two-column markdown table of severity →
// count. A row is always emitted for every severity (including zeros) so
// re-reviews show consistent shape.
func renderSeverityTable(counts map[scoring.Severity]int) string {
	var b strings.Builder
	b.WriteString("| Severity | Count |\n| --- | --- |\n")
	for _, s := range severityOrder {
		fmt.Fprintf(&b, "| %s | %d |\n", s, counts[s])
	}
	return b.String()
}

// labelSet flattens Labels into a de-duplicated list suitable for
// AddLabels. PRType and RiskTag are single labels; Domains and
// OwnershipHints expand. Empty entries are dropped.
func labelSet(l model.Labels) []string {
	seen := make(map[string]struct{})
	push := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		seen[v] = struct{}{}
	}
	push(l.PRType)
	push(l.RiskTag)
	for _, d := range l.Domains {
		push(d)
	}
	for _, o := range l.OwnershipHints {
		push(o)
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// scoreCountsFromMap buckets scoring.Score values by Severity for the PR
// description table. Kept here (not in review_cmd.go) because it's a pure
// helper the description tests want to exercise directly.
func scoreCountsFromMap(scores map[string]scoring.Score) map[scoring.Severity]int {
	out := make(map[scoring.Severity]int, len(severityOrder))
	for _, s := range severityOrder {
		out[s] = 0
	}
	for _, sc := range scores {
		out[sc.Severity]++
	}
	return out
}
