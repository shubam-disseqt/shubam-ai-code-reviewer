// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
	"github.com/shubam-disseqt/z-code-reviewer/internal/overlap"
	"github.com/shubam-disseqt/z-code-reviewer/internal/scoring"
)

// zreviewBlockBegin / zreviewBlockEnd wrap the managed PR description block
// so re-running the reviewer replaces it in place. Anything outside the
// markers is preserved verbatim — humans and other bots can co-exist.
const (
	zreviewBlockBegin = "<!-- ZREVIEW:BEGIN -->"
	zreviewBlockEnd   = "<!-- ZREVIEW:END -->"
)

// prBodyClient is the narrow interface UpdateDescription needs from the
// GitHub client. Kept local so description_test.go can supply a fake
// without touching the real HTTP layer.
type prBodyClient interface {
	GetPRBody(ctx context.Context, owner, repo string, number int) (string, error)
	UpdatePRBody(ctx context.Context, owner, repo string, number int, body string) error
	AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error
}

// UpdateDescription reads the current PR body, replaces (or appends) the
// zreview-managed block, and writes it back. Labels derived from the Phase
// 15 Labels payload are added idempotently. All errors are wrapped with
// stage prefixes so a failure names the operation that hit it.
//
// scoreCounts is the tally of surviving findings by Severity (SUPPRESS
// excluded) — the caller computes it once from the same emit stream so the
// PR description matches what the reviewer actually published.
func UpdateDescription(
	ctx context.Context,
	client prBodyClient,
	owner, repo string, pr int,
	summary model.Summary,
	labels model.Labels,
	scoreCounts map[scoring.Severity]int,
	overlapFindings []overlap.Finding,
	scannerFindings []model.LlmComment,
) error {
	if client == nil {
		return fmt.Errorf("update description: nil client")
	}
	if owner == "" || repo == "" || pr == 0 {
		return fmt.Errorf("update description: owner/repo/pr required")
	}

	current, err := client.GetPRBody(ctx, owner, repo, pr)
	if err != nil {
		return fmt.Errorf("update description: %w", err)
	}
	updated := replaceZreviewBlock(current, renderZreviewBlock(summary, labels, scoreCounts, overlapFindings, scannerFindings))
	if updated != current {
		if err := client.UpdatePRBody(ctx, owner, repo, pr, updated); err != nil {
			return fmt.Errorf("update description: %w", err)
		}
	}
	if lbls := labelSet(labels); len(lbls) > 0 {
		if err := client.AddLabels(ctx, owner, repo, pr, lbls); err != nil {
			return fmt.Errorf("update description: %w", err)
		}
	}
	return nil
}

// replaceZreviewBlock swaps the content between the ZREVIEW markers with
// `block`. If markers are absent it appends a fresh block, separated by a
// blank line when the existing body is non-empty. `block` should NOT
// include the marker lines — this function adds them.
func replaceZreviewBlock(body, block string) string {
	wrapped := zreviewBlockBegin + "\n" + block + "\n" + zreviewBlockEnd

	beginIdx := strings.Index(body, zreviewBlockBegin)
	endIdx := strings.Index(body, zreviewBlockEnd)
	if beginIdx >= 0 && endIdx > beginIdx {
		before := body[:beginIdx]
		after := body[endIdx+len(zreviewBlockEnd):]
		return before + wrapped + after
	}
	if strings.TrimSpace(body) == "" {
		return wrapped
	}
	// Preserve existing content, append the block after one blank line.
	trimmed := strings.TrimRight(body, "\n")
	return trimmed + "\n\n" + wrapped
}

// renderZreviewBlock builds the markdown that goes between the two markers.
// Kept intentionally small: a walkthrough paragraph, a severity table, the
// risk tag, and a change-groups list. Every section is optional so a
// summary that came back mostly-empty still produces a sane block.
func renderZreviewBlock(summary model.Summary, labels model.Labels, counts map[scoring.Severity]int, overlapFindings []overlap.Finding, scannerFindings []model.LlmComment) string {
	var b strings.Builder
	b.WriteString("## Automated review by zreview\n\n")

	if summary.Walkthrough != "" {
		b.WriteString(strings.TrimSpace(summary.Walkthrough))
		b.WriteString("\n\n")
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
