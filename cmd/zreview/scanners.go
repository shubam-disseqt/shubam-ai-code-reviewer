// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/shubam-disseqt/z-code-reviewer/internal/logutil"
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
	"github.com/shubam-disseqt/z-code-reviewer/internal/scanner"
)

// runScanners runs the deterministic scanner runner against the kept files.
// A total failure at the runner level (only ctx cancellation triggers it)
// is treated as non-fatal — the review continues without scanner findings.
// The per-scanner best-effort semantics live inside internal/scanner.
func runScanners(ctx context.Context, repo string, kept []model.Diff, logger *slog.Logger) []scanner.ScannerFinding {
	paths := scannerPathsFromDiffs(kept)
	// Two stages: "scanner" for the per-adapter chatter (Debug — noisy on a
	// full run), "scanners" for the pipeline summary (Info — must show up).
	adapterLog := logutil.WithStage(logger, "scanner")
	pipelineLog := logutil.WithStage(logger, "scanners")
	findings, err := scanner.Run(ctx, repo, paths, scanner.Options{
		Disabled: scanner.EnvDisabled(),
		Stderr:   io.Discard,
		Log: func(format string, args ...any) {
			adapterLog.Debug(fmt.Sprintf(format, args...))
		},
	})
	if err != nil {
		pipelineLog.Warn("continuing without scanner findings", "err", err.Error())
		return nil
	}
	tally := scanner.TallyByTool(findings)
	pipelineLog.Info(
		fmt.Sprintf("%d findings from %s", len(findings), tally),
		"count", len(findings),
		"by_tool", tally,
	)
	return findings
}

// scannerPathsFromDiffs mirrors changedPathsFromDiffs but is defined here
// so scanners.go stays self-contained. Duplicates one 8-line helper on
// purpose — the alternative is exporting the private helper across files
// for one caller.
// ponytail: duplicate helper, fold into shared file if a third caller shows up.
func scannerPathsFromDiffs(kept []model.Diff) []string {
	paths := make([]string, 0, len(kept))
	for _, d := range kept {
		p := d.NewPath
		if p == "" || p == "/dev/null" {
			p = d.OldPath
		}
		if p == "" {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

// groupScannerFindingsByPath indexes findings by their target Path so the
// prompt builder can render only the findings relevant to the file under
// review.
func groupScannerFindingsByPath(findings []scanner.ScannerFinding) map[string][]scanner.ScannerFinding {
	if len(findings) == 0 {
		return nil
	}
	out := make(map[string][]scanner.ScannerFinding, len(findings))
	for _, f := range findings {
		if f.Path == "" {
			continue
		}
		out[f.Path] = append(out[f.Path], f)
	}
	return out
}

// scannerFindingToComment lifts a deterministic scanner finding into the
// LlmComment shape the collector, dedup, and emitter already understand.
// Source is set to "scanner:<tool>" per the Phase 14 contract; Phase 16
// scoring and Phase 17 SARIF routing consume that prefix.
func scannerFindingToComment(f scanner.ScannerFinding) model.LlmComment {
	line := f.Line
	if line < 1 {
		line = 1
	}
	content := f.Description
	if content == "" {
		content = fmt.Sprintf("%s finding: %s", f.Tool, f.RuleID)
	}
	if f.Evidence != "" {
		content = content + "\n\n" + "Evidence: `" + strings.ReplaceAll(f.Evidence, "`", "'") + "`"
	}
	return model.LlmComment{
		Path:      f.Path,
		Content:   content,
		StartLine: line,
		EndLine:   line,
		Category:  scannerKindToCategory(f.Kind),
		Severity:  scannerSeverityToComment(f.Severity),
		Source:    "scanner:" + f.Tool,
	}
}

// scannerKindToCategory maps scanner.Kind onto the LlmComment.Category
// vocabulary. All three kinds live in the security bucket for now — the
// scoring engine can split them later.
func scannerKindToCategory(k scanner.Kind) string {
	switch k {
	case scanner.KindSecret, scanner.KindSAST, scanner.KindCVE:
		return "security"
	default:
		return "security"
	}
}

// scannerSeverityToComment maps the scanner's uppercase severity onto the
// lowercase severity vocabulary used elsewhere in LlmComment.
func scannerSeverityToComment(s scanner.Severity) string {
	return strings.ToLower(string(s))
}

// renderKnownIssuesBlock produces the optional "## Known Issues (from
// static analysis)" block for the given file. Returns "" when there are
// no findings so the prompt template can skip the section entirely.
func renderKnownIssuesBlock(findings []scanner.ScannerFinding) string {
	if len(findings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Known Issues (from static analysis)\n\n")
	b.WriteString("The following issues were detected deterministically by static analysis. Do not re-report them. If you have relevant context, extend the finding with a one-line risk note; otherwise leave them for the scoring engine to publish.\n\n")
	for _, f := range findings {
		fmt.Fprintf(&b, "- [%s / %s / %s] %s:%d — %s\n",
			f.Tool, f.Kind, f.Severity, f.Path, f.Line, f.Description)
	}
	return b.String()
}
