// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package sarif

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/scoring"
)

// update rewrites golden files when set. Run `go test -update ./internal/sarif`
// to regenerate.
var update = flag.Bool("update", false, "rewrite testdata/*.golden.json fixtures")

func TestEncode_Golden(t *testing.T) {
	cases := []struct {
		name     string
		findings []Finding
		meta     Meta
		golden   string
	}{
		{
			name:     "empty",
			findings: nil,
			meta:     Meta{Repo: "shubam-disseqt/z-code-reviewer", HeadSHA: "deadbeef", Version: "v0.2.0"},
			golden:   "empty.golden.json",
		},
		{
			name: "scanner_findings",
			findings: []Finding{
				{
					Source:      "scanner:gitleaks",
					RuleID:      "aws-access-token",
					Description: "hardcoded AWS access key",
					Path:        "cmd/app/main.go",
					StartLine:   42,
					EndLine:     42,
					Severity:    scoring.SeverityHigh,
					Fingerprint: "fp-aws-1",
				},
				{
					Source:      "scanner:semgrep",
					RuleID:      "go.lang.security.audit.crypto.md5-used",
					Description: "MD5 used for crypto",
					Path:        "internal/auth/hash.go",
					StartLine:   10,
					EndLine:     14,
					Severity:    scoring.SeverityMedium,
					Fingerprint: "fp-md5-1",
				},
				{
					Source:      "scanner:govulncheck",
					RuleID:      "GO-2024-1234",
					Description: "net/http path traversal",
					Path:        "internal/server/handler.go",
					StartLine:   77,
					Severity:    scoring.SeverityCritical,
					Fingerprint: "fp-cve-1",
					HelpURI:     "https://pkg.go.dev/vuln/GO-2024-1234",
				},
			},
			meta:   Meta{Repo: "shubam-disseqt/z-code-reviewer", HeadSHA: "cafef00d", Version: "v0.2.0"},
			golden: "scanner_findings.golden.json",
		},
		{
			name: "suppress_dropped",
			findings: []Finding{
				{Source: "scanner:semgrep", RuleID: "kept", Description: "kept", Path: "a.go", StartLine: 1, Severity: scoring.SeverityLow, Fingerprint: "fp-1"},
				{Source: "scanner:semgrep", RuleID: "gone", Description: "gone", Path: "a.go", StartLine: 2, Severity: scoring.SeveritySuppress, Fingerprint: "fp-2"},
			},
			meta:   Meta{Version: "v0.2.0"},
			golden: "suppress_dropped.golden.json",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Encode(tc.findings, tc.meta)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			path := filepath.Join("testdata", tc.golden)
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %s: %v", path, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", tc.golden, got, want)
			}
		})
	}
}

func TestEncode_EmptyResultsArray(t *testing.T) {
	// An empty findings slice must serialise `results: []` — not `null` and
	// not omitted. GitHub Code Scanning treats a null results field as an
	// upload error.
	out, err := Encode(nil, Meta{Version: "v0"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var parsed struct {
		Runs []struct {
			Results []Result `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Runs) != 1 {
		t.Fatalf("runs=%d", len(parsed.Runs))
	}
	if parsed.Runs[0].Results == nil {
		t.Errorf("results is nil, want non-nil empty slice")
	}
	if len(parsed.Runs[0].Results) != 0 {
		t.Errorf("results len=%d, want 0", len(parsed.Runs[0].Results))
	}
	// Confirm the on-wire form is `"results": []`.
	if !bytes.Contains(out, []byte("\"results\": []")) {
		t.Errorf("expected `\"results\": []` in output, got:\n%s", out)
	}
}

func TestEncode_UnknownSeverityFallsBackToWarning(t *testing.T) {
	out, err := Encode([]Finding{{
		Source:      "scanner:x",
		RuleID:      "r1",
		Description: "d",
		Path:        "a.go",
		StartLine:   1,
		Severity:    "MYSTERY",
	}}, Meta{Version: "v0"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Contains(out, []byte("\"level\": \"warning\"")) {
		t.Errorf("expected warning fallback, got:\n%s", out)
	}
}

func TestEncode_PartialFingerprint(t *testing.T) {
	out, err := Encode([]Finding{{
		Source:      "scanner:gitleaks",
		RuleID:      "aws",
		Description: "d",
		Path:        "a.go",
		StartLine:   1,
		Severity:    scoring.SeverityHigh,
		Fingerprint: "abc123",
	}}, Meta{Version: "v0"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// The zreview/v1 partialFingerprints key is load-bearing for Code
	// Scanning alert carry-over. Guard the key + value shape.
	if !bytes.Contains(out, []byte("\"zreview/v1\": \"abc123\"")) {
		t.Errorf("partialFingerprints missing, got:\n%s", out)
	}
}

func TestEncode_RuleDeduplication(t *testing.T) {
	// Two findings sharing the same (Source, RuleID) should yield exactly
	// one ReportingDescriptor in the driver's rules array.
	out, err := Encode([]Finding{
		{Source: "scanner:gitleaks", RuleID: "aws", Description: "one", Path: "a.go", StartLine: 1, Severity: scoring.SeverityHigh},
		{Source: "scanner:gitleaks", RuleID: "aws", Description: "two", Path: "b.go", StartLine: 2, Severity: scoring.SeverityHigh},
	}, Meta{Version: "v0"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var log Log
	if err := json.Unmarshal(out, &log); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := len(log.Runs[0].Tool.Driver.Rules); got != 1 {
		t.Errorf("rules len = %d, want 1", got)
	}
	if got := len(log.Runs[0].Results); got != 2 {
		t.Errorf("results len = %d, want 2", got)
	}
}
