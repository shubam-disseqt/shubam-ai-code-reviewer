// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
	"github.com/shubam-disseqt/z-code-reviewer/internal/scanner"
)

func TestScannerFindingToComment(t *testing.T) {
	f := scanner.ScannerFinding{
		Tool:        "gitleaks",
		RuleID:      "aws-access-token",
		Path:        "config/aws.py",
		Line:        5,
		Kind:        scanner.KindSecret,
		Severity:    scanner.SeverityHigh,
		Description: "AWS Access Token",
		Evidence:    "key=***REDACTED***",
	}
	got := scannerFindingToComment(f)
	if got.Path != "config/aws.py" {
		t.Errorf("Path: got %q", got.Path)
	}
	if got.StartLine != 5 || got.EndLine != 5 {
		t.Errorf("Lines: got %d,%d", got.StartLine, got.EndLine)
	}
	if got.Source != "scanner:gitleaks" {
		t.Errorf("Source: got %q", got.Source)
	}
	if got.Category != "security" {
		t.Errorf("Category: got %q", got.Category)
	}
	if got.Severity != "high" {
		t.Errorf("Severity: got %q", got.Severity)
	}
	if !strings.Contains(got.Content, "AWS Access Token") {
		t.Errorf("Content missing description: %q", got.Content)
	}
	if !strings.Contains(got.Content, "Evidence") {
		t.Errorf("Content missing evidence: %q", got.Content)
	}
}

func TestScannerFindingToCommentClampsLine(t *testing.T) {
	got := scannerFindingToComment(scanner.ScannerFinding{Tool: "semgrep", Path: "a.go", Line: 0})
	if got.StartLine != 1 || got.EndLine != 1 {
		t.Errorf("expected line clamped to 1, got %d,%d", got.StartLine, got.EndLine)
	}
}

func TestGroupScannerFindingsByPath(t *testing.T) {
	got := groupScannerFindingsByPath([]scanner.ScannerFinding{
		{Tool: "gitleaks", Path: "a.go", Line: 1},
		{Tool: "semgrep", Path: "a.go", Line: 2},
		{Tool: "semgrep", Path: "b.go", Line: 3},
		{Tool: "govulncheck", Path: "", Line: 0}, // dropped: empty path
	})
	if len(got["a.go"]) != 2 {
		t.Errorf("want 2 findings for a.go, got %d", len(got["a.go"]))
	}
	if len(got["b.go"]) != 1 {
		t.Errorf("want 1 finding for b.go, got %d", len(got["b.go"]))
	}
	if _, ok := got[""]; ok {
		t.Errorf("empty-path finding leaked into map")
	}
}

func TestRenderKnownIssuesBlockEmpty(t *testing.T) {
	if got := renderKnownIssuesBlock(nil); got != "" {
		t.Errorf("nil input should return empty, got %q", got)
	}
	if got := renderKnownIssuesBlock([]scanner.ScannerFinding{}); got != "" {
		t.Errorf("empty slice should return empty, got %q", got)
	}
}

func TestRenderKnownIssuesBlockContent(t *testing.T) {
	got := renderKnownIssuesBlock([]scanner.ScannerFinding{
		{Tool: "gitleaks", Kind: scanner.KindSecret, Severity: scanner.SeverityHigh,
			Path: "a.go", Line: 3, Description: "AWS key leaked"},
	})
	if !strings.Contains(got, "Known Issues (from static analysis)") {
		t.Errorf("header missing: %q", got)
	}
	if !strings.Contains(got, "gitleaks") || !strings.Contains(got, "AWS key leaked") {
		t.Errorf("body incomplete: %q", got)
	}
	if !strings.Contains(got, "a.go:3") {
		t.Errorf("path:line missing: %q", got)
	}
}

func TestScannerPathsFromDiffs(t *testing.T) {
	got := scannerPathsFromDiffs([]model.Diff{
		{NewPath: "a.go"},
		{OldPath: "removed.go", NewPath: "/dev/null"},
		{NewPath: "b.go"},
		{}, // fully-empty diff — skipped
	})
	want := []string{"a.go", "removed.go", "b.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRunScannersNoBinariesIsNonFatal(t *testing.T) {
	// With no real binaries invoked, the runner still returns cleanly;
	// each production adapter prints a "skipping X (not installed)"
	// line via the logger. This is the property the review pipeline
	// relies on to keep working on runners that ship without scanners.
	var buf bytes.Buffer
	got := runScanners(context.Background(), t.TempDir(), []model.Diff{{NewPath: "a.go"}}, testLogger(&buf))
	if got != nil && len(got) != 0 {
		// If a scanner IS installed on the test machine it may return
		// zero findings on an empty tempdir; we accept either.
		t.Logf("scanners returned %d findings (machine has binaries installed)", len(got))
	}
	if !strings.Contains(buf.String(), "[zreview] scanners:") {
		t.Errorf("expected summary log line, got: %s", buf.String())
	}
}
