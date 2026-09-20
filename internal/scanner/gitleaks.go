// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// gitleaksScanner shells out to `gitleaks detect` and parses its JSON report.
// gitleaks writes a JSON array of finding objects to --report-path; it does
// NOT stream to stdout in the modern report-path mode, hence the tmpfile
// dance.
type gitleaksScanner struct {
	stderr io.Writer
}

func (s *gitleaksScanner) Name() string { return "gitleaks" }

// gitleaksFinding mirrors the on-disk JSON. gitleaks 8.x uses these field
// names; earlier versions used snake_case. Fields we don't need are omitted.
type gitleaksFinding struct {
	RuleID      string `json:"RuleID"`
	Description string `json:"Description"`
	File        string `json:"File"`
	StartLine   int    `json:"StartLine"`
	Match       string `json:"Match"`
	Secret      string `json:"Secret"`
}

func (s *gitleaksScanner) Run(ctx context.Context, repoRoot string, _ []string) ([]ScannerFinding, error) {
	if _, err := exec.LookPath("gitleaks"); err != nil {
		return nil, fmt.Errorf("skipping gitleaks (not installed)")
	}

	tmp, err := os.CreateTemp("", "sacr-gitleaks-*.json")
	if err != nil {
		return nil, fmt.Errorf("tempfile: %w", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	// --no-git makes gitleaks scan the working tree directly rather than
	// the git history. --exit-code 0 stops it from returning non-zero
	// when findings exist (we treat findings as data, not an error).
	err = execAttempt(ctx, func() error {
		//nolint:gosec // args are all constants except the paths, which we control
		cmd := exec.CommandContext(ctx, "gitleaks", "detect",
			"--source", repoRoot,
			"--report-format", "json",
			"--report-path", tmp.Name(),
			"--no-git",
			"--exit-code", "0",
		)
		cmd.Stderr = s.stderr
		return cmd.Run()
	})
	if err != nil {
		return nil, fmt.Errorf("gitleaks exec: %w", err)
	}

	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}
	// Empty report from a clean run is either an empty array or missing.
	if len(data) == 0 {
		return nil, nil
	}
	var raw []gitleaksFinding
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse report: %w", err)
	}
	return convertGitleaks(raw, repoRoot), nil
}

// convertGitleaks maps gitleaks findings into the normalised shape. Split
// out so tests can drive it directly from a fixture without spawning the
// binary.
func convertGitleaks(raw []gitleaksFinding, repoRoot string) []ScannerFinding {
	out := make([]ScannerFinding, 0, len(raw))
	for _, f := range raw {
		path := normalisePath(f.File, repoRoot)
		out = append(out, ScannerFinding{
			Tool:        "gitleaks",
			RuleID:      f.RuleID,
			Path:        path,
			Line:        f.StartLine,
			Kind:        KindSecret,
			Severity:    SeverityHigh,
			Description: gitleaksDescription(f),
			Evidence:    redactSecret(f.Match, f.Secret),
		})
	}
	return out
}

// gitleaksDescription prefers the tool's own Description if present; falls
// back to the rule ID for older reports that omit it.
func gitleaksDescription(f gitleaksFinding) string {
	if f.Description != "" {
		return f.Description
	}
	return fmt.Sprintf("gitleaks rule %q matched", f.RuleID)
}

// redactSecret replaces the secret substring inside match with a fixed
// asterisk block so the finding can be safely surfaced in a review comment
// without leaking the credential. Falls back to a fully-redacted marker
// when the secret isn't a clean substring of the match.
func redactSecret(match, secret string) string {
	const mask = "***REDACTED***"
	if secret == "" {
		return match
	}
	if match == "" {
		return mask
	}
	// Byte-level replace is fine: gitleaks matches are raw bytes from the
	// source file, and we're only substituting a literal ASCII substring.
	for i := 0; i+len(secret) <= len(match); i++ {
		if match[i:i+len(secret)] == secret {
			return match[:i] + mask + match[i+len(secret):]
		}
	}
	return mask
}

// normalisePath returns a repo-relative path when abs falls inside
// repoRoot; otherwise returns abs unchanged.
func normalisePath(abs, repoRoot string) string {
	if abs == "" || repoRoot == "" {
		return abs
	}
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil || len(rel) == 0 || rel[0] == '.' {
		return abs
	}
	return filepath.ToSlash(rel)
}
