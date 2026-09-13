// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package scanner wraps deterministic security scanners (gitleaks, semgrep,
// govulncheck) as best-effort adapters. Each adapter shells out to the tool,
// parses its JSON output, and returns a normalised ScannerFinding stream.
// A missing binary or a parse error is logged and skipped — only ctx
// cancellation surfaces as an error at the runner level.
package scanner

// Kind classifies a scanner finding by the category of issue it reports.
// One of: secret | sast | cve.
type Kind string

const (
	KindSecret Kind = "secret"
	KindSAST   Kind = "sast"
	KindCVE    Kind = "cve"
)

// Severity is the normalised severity label. Individual tools use their own
// vocabularies (gitleaks has no severity, semgrep uses ERROR/WARNING/INFO,
// govulncheck has no severity per finding); adapters map into this set.
type Severity string

const (
	SeverityCritical Severity = "CRITICAL"
	SeverityHigh     Severity = "HIGH"
	SeverityMedium   Severity = "MEDIUM"
	SeverityLow      Severity = "LOW"
	SeverityInfo     Severity = "INFO"
)

// ScannerFinding is the normalised finding shape emitted by every adapter.
// It is intentionally flat so the downstream comment collector can carry it
// without knowing which tool produced it. Phase 16 scoring will consume the
// same shape.
type ScannerFinding struct {
	// Tool is the producing binary name — e.g. "gitleaks", "semgrep",
	// "govulncheck". Used to build Source="scanner:<Tool>" on the LlmComment.
	Tool string
	// RuleID is the tool-specific rule identifier — e.g. "aws-access-token",
	// "GO-2024-1234", or a semgrep check_id. Stable across invocations of the
	// same tool version.
	RuleID string
	// Path is the repository-relative path the finding refers to. May be
	// empty for tool-level errors that don't attach to a file.
	Path string
	// Line is the 1-indexed line number of the finding. 0 means unknown.
	Line int
	// Kind is one of secret | sast | cve.
	Kind Kind
	// Severity is the normalised severity label.
	Severity Severity
	// Description is a one-line human-readable summary suitable for a
	// review comment.
	Description string
	// Evidence is an optional short excerpt (redacted for secrets) that
	// helps a reviewer locate the finding. Adapters MUST redact raw
	// secrets before populating this field.
	Evidence string
}
