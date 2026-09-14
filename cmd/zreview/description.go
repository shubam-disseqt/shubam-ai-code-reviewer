// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/shubam-disseqt/z-code-reviewer/internal/effort"
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
	// Used to clean up stale exclusive labels (e.g. an old risk/* tag)
	// before applying the fresh set. Both methods are best-effort.
	ListLabels(ctx context.Context, owner, repo string, number int) ([]string, error)
	RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error
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
	effortScore effort.Score,
	pkgDiagram string,
) error {
	if client == nil {
		return fmt.Errorf("update description: nil client")
	}
	if owner == "" || repo == "" || pr == 0 {
		return fmt.Errorf("update description: owner/repo/pr required")
	}

	// Escalate the labeler's risk tag when scanner or scoring evidence
	// says the real risk is higher. The labeler only reads the diff, so a
	// PR that adds a hardcoded secret gets tagged risk/low ("just a
	// config file") even though a HIGH-severity gitleaks finding lives
	// inside it. This is UX confusion the operator would hit on day one.
	labels = escalateRiskFromFindings(labels, scoreCounts, scannerFindings)

	current, err := client.GetPRBody(ctx, owner, repo, pr)
	if err != nil {
		return fmt.Errorf("update description: %w", err)
	}
	updated := replaceZreviewBlock(current, renderZreviewBlock(summary, labels, scoreCounts, overlapFindings, scannerFindings, effortScore, pkgDiagram))
	if updated != current {
		if err := client.UpdatePRBody(ctx, owner, repo, pr, updated); err != nil {
			return fmt.Errorf("update description: %w", err)
		}
	}
	if lbls := labelSet(labels); len(lbls) > 0 {
		// Clean up stale exclusive labels (risk/*) before applying the
		// fresh set. Missing labels or a list error is a no-op — we still
		// try to add.
		if existing, err := client.ListLabels(ctx, owner, repo, pr); err == nil {
			for _, drop := range staleExclusiveLabels(existing, lbls) {
				_ = client.RemoveLabel(ctx, owner, repo, pr, drop)
			}
		}
		if err := client.AddLabels(ctx, owner, repo, pr, lbls); err != nil {
			return fmt.Errorf("update description: %w", err)
		}
	}
	return nil
}

// staleExclusiveLabels returns the subset of `existing` that belongs to an
// exclusive namespace (currently `risk/*`) and is NOT in the fresh set. The
// labeler emits at most one per namespace, so any others are stale. Non-
// exclusive labels (domains, ownership hints, PR type) are additive and
// left alone.
func staleExclusiveLabels(existing, fresh []string) []string {
	freshSet := make(map[string]struct{}, len(fresh))
	for _, f := range fresh {
		freshSet[strings.ToLower(f)] = struct{}{}
	}
	var drop []string
	for _, e := range existing {
		lower := strings.ToLower(e)
		if !strings.HasPrefix(lower, "risk/") {
			continue
		}
		if _, keep := freshSet[lower]; keep {
			continue
		}
		drop = append(drop, e)
	}
	return drop
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
func renderZreviewBlock(summary model.Summary, labels model.Labels, counts map[scoring.Severity]int, overlapFindings []overlap.Finding, scannerFindings []model.LlmComment, effortScore effort.Score, pkgDiagram string) string {
	var b strings.Builder
	b.WriteString("## Automated review by zreview\n\n")

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
	b.WriteString("\n_Reproducible: same diff → same score. Weights configurable via `.zreview/effort.yaml` or `$ZREVIEW_EFFORT_POLICY`._\n")
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
