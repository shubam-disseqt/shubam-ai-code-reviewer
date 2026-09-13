// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// semgrepScanner shells out to semgrep and parses the top-level results
// list from its --json output.
type semgrepScanner struct {
	stderr io.Writer
}

func (s *semgrepScanner) Name() string { return "semgrep" }

// semgrepResult mirrors one entry in the semgrep JSON `results` array.
// We deliberately keep only fields we surface — semgrep exposes ~30 keys
// per result but most are for its own CI product, not for review comments.
type semgrepResult struct {
	CheckID string          `json:"check_id"`
	Path    string          `json:"path"`
	Start   semgrepLocation `json:"start"`
	End     semgrepLocation `json:"end"`
	Extra   semgrepExtra    `json:"extra"`
}

type semgrepLocation struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

type semgrepExtra struct {
	Message  string `json:"message"`
	Severity string `json:"severity"`
	Lines    string `json:"lines"`
}

type semgrepOutput struct {
	Results []semgrepResult `json:"results"`
}

func (s *semgrepScanner) Run(ctx context.Context, repoRoot string, changedPaths []string) ([]ScannerFinding, error) {
	if _, err := exec.LookPath("semgrep"); err != nil {
		return nil, fmt.Errorf("skipping semgrep (not installed)")
	}

	// Prefer to scan only the changed paths to keep semgrep bounded on
	// large repos. If the caller passed no paths, fall back to the whole
	// repo so a broken caller doesn't accidentally silence the scanner.
	targets := changedPaths
	if len(targets) == 0 {
		targets = []string{repoRoot}
	}

	args := []string{
		"--config", "auto",
		"--json",
		"--quiet",
		"--disable-version-check",
		"--metrics=off",
	}
	args = append(args, targets...)

	//nolint:gosec // targets are repo-relative paths from the deterministic selector
	cmd := exec.CommandContext(ctx, "semgrep", args...)
	cmd.Dir = repoRoot
	cmd.Stderr = s.stderr
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// semgrep exits non-zero when it finds issues (with --error)
		// OR when config download fails. We didn't set --error so any
		// non-zero exit is a real failure; report + skip.
		return nil, fmt.Errorf("semgrep exec: %w", err)
	}

	if strings.TrimSpace(stdout.String()) == "" {
		return nil, nil
	}
	var parsed semgrepOutput
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("parse output: %w", err)
	}
	return convertSemgrep(parsed.Results), nil
}

// convertSemgrep maps parsed results into normalised findings. Extracted
// so unit tests can drive the mapping without invoking semgrep itself.
func convertSemgrep(results []semgrepResult) []ScannerFinding {
	out := make([]ScannerFinding, 0, len(results))
	for _, r := range results {
		out = append(out, ScannerFinding{
			Tool:        "semgrep",
			RuleID:      r.CheckID,
			Path:        r.Path,
			Line:        r.Start.Line,
			Kind:        KindSAST,
			Severity:    mapSemgrepSeverity(r.Extra.Severity),
			Description: firstNonEmpty(r.Extra.Message, r.CheckID),
			Evidence:    strings.TrimSpace(r.Extra.Lines),
		})
	}
	return out
}

// mapSemgrepSeverity maps semgrep's three-level severity onto our
// five-level scale. Semgrep does not distinguish critical from high — we
// leave critical for CVE / secret findings where the notion is meaningful.
func mapSemgrepSeverity(raw string) Severity {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "ERROR":
		return SeverityHigh
	case "WARNING":
		return SeverityMedium
	case "INFO":
		return SeverityInfo
	default:
		return SeverityMedium
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
