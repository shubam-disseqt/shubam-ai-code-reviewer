// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package sarif

import (
	"strings"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/scoring"
)

// TestValidate_AcceptsEncoderOutput is the load-bearing check: whatever
// Encode produces (empty run, populated run, dedup case) must Validate
// clean. If it doesn't, our own uploader would 422.
func TestValidate_AcceptsEncoderOutput(t *testing.T) {
	cases := [][]Finding{
		nil,
		{{Source: "scanner:gitleaks", RuleID: "aws", Description: "d", Path: "a.go", StartLine: 1, Severity: scoring.SeverityHigh}},
		{
			{Source: "scanner:x", RuleID: "r1", Description: "d1", Path: "a.go", StartLine: 1, EndLine: 3, Severity: scoring.SeverityCritical},
			{Source: "scanner:x", RuleID: "r2", Description: "d2", Path: "b.go", StartLine: 42, Severity: scoring.SeverityLow},
		},
	}
	for i, findings := range cases {
		blob, err := Encode(findings, Meta{Version: "v0"})
		if err != nil {
			t.Fatalf("case %d encode: %v", i, err)
		}
		if err := Validate(blob); err != nil {
			t.Errorf("case %d Validate: %v", i, err)
		}
	}
}

func TestValidate_RejectsBadInputs(t *testing.T) {
	tests := []struct {
		name string
		blob string
		want string
	}{
		{"malformed json", `not json`, "unmarshal"},
		{"wrong version", `{"version":"1.0.0","runs":[{"tool":{"driver":{"name":"x","rules":[]}},"results":[]}]}`, "version"},
		{"empty runs", `{"version":"2.1.0","runs":[]}`, "runs must be non-empty"},
		{"missing driver name", `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"","rules":[]}},"results":[]}]}`, "driver.name"},
		{"missing ruleId", `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"x","rules":[]}},"results":[{"ruleId":"","level":"warning","message":{"text":"m"}}]}]}`, "ruleId"},
		{"bad level", `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"x","rules":[]}},"results":[{"ruleId":"r","level":"catastrophic","message":{"text":"m"}}]}]}`, "level"},
		{"zero startLine", `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"x","rules":[]}},"results":[{"ruleId":"r","level":"warning","message":{"text":"m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"a.go"},"region":{"startLine":0}}}]}]}]}`, "startLine"},
		{"results null literal", `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"x","rules":[]}},"results": null}]}`, "results must be []"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate([]byte(tc.blob))
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want contains %q", err, tc.want)
			}
		})
	}
}
