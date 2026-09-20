// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scoring

import (
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// testPolicy returns a compact policy suitable for table-driven tests.
// It intentionally excludes real-world categories so cases can assert
// fallback behaviour without fighting the shipped default policy.
func testPolicy() Policy {
	return Policy{
		Categories: map[string]PolicyEntry{
			"security.hardcoded-secret": {
				Impact:          0.95,
				ConfidenceFloor: 0.90,
				SeverityMap: map[string]Severity{
					"high":    SeverityCritical,
					"medium":  SeverityHigh,
					"low":     SeverityHigh,
					"default": SeverityCritical,
				},
			},
			"security": {
				Impact:          0.85,
				ConfidenceFloor: 0.70,
				SeverityMap: map[string]Severity{
					"high":    SeverityHigh,
					"medium":  SeverityHigh,
					"default": SeverityHigh,
				},
			},
			"maintainability.naming": {
				Impact:          0.10,
				ConfidenceFloor: 0.60,
				SeverityMap: map[string]Severity{
					"default": SeveritySuppress,
				},
			},
			"secret": {
				Impact:          0.95,
				ConfidenceFloor: 0.90,
				SeverityMap: map[string]Severity{
					"critical": SeverityCritical,
					"default":  SeverityCritical,
				},
			},
		},
		Defaults: PolicyEntry{
			Impact:          0.40,
			ConfidenceFloor: 0.50,
			SeverityMap: map[string]Severity{
				"high":    SeverityHigh,
				"default": SeverityMedium,
			},
		},
	}
}

func TestScoreSeverityBuckets(t *testing.T) {
	pol := testPolicy()
	cases := []struct {
		name    string
		comment model.LlmComment
		want    Severity
	}{
		{
			name:    "hardcoded secret with high raw severity",
			comment: model.LlmComment{Category: "security.hardcoded-secret", Severity: "high"},
			want:    SeverityCritical,
		},
		{
			name:    "hardcoded secret with low raw severity still critical (mapped)",
			comment: model.LlmComment{Category: "security.hardcoded-secret", Severity: "low"},
			want:    SeverityHigh,
		},
		{
			name:    "missing-auth uses default entry via category prefix",
			comment: model.LlmComment{Category: "security.missing-auth", Severity: ""},
			want:    SeverityHigh, // falls back to "security" bucket → default
		},
		{
			name:    "maintainability.naming suppresses",
			comment: model.LlmComment{Category: "maintainability.naming", Severity: "low"},
			want:    SeveritySuppress,
		},
		{
			name:    "unmapped raw severity uses entry default",
			comment: model.LlmComment{Category: "security", Severity: "purple"},
			want:    SeverityHigh,
		},
		{
			name:    "unknown category falls back to defaults",
			comment: model.LlmComment{Category: "unknown", Severity: "high"},
			want:    SeverityHigh,
		},
		{
			name:    "empty category and severity fall back to defaults default",
			comment: model.LlmComment{},
			want:    SeverityMedium,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := ScoreOne(tt.comment, pol)
			if got.Score.Severity != tt.want {
				t.Errorf("severity: got %q want %q (rationale=%s)", got.Score.Severity, tt.want, got.Score.Rationale)
			}
		})
	}
}

func TestScoreConfidenceBumps(t *testing.T) {
	pol := testPolicy()
	baseline := pol.Categories["security"].ConfidenceFloor // 0.70

	tests := []struct {
		name    string
		comment model.LlmComment
		wantMin float64
		wantMax float64
	}{
		{
			name:    "plain LLM finding gets floor",
			comment: model.LlmComment{Category: "security", Severity: "high"},
			wantMin: baseline,
			wantMax: baseline,
		},
		{
			name:    "scanner source gets +0.05",
			comment: model.LlmComment{Category: "security", Severity: "high", Source: "scanner:gitleaks"},
			wantMin: baseline + scannerConfidenceBump - 0.0001,
			wantMax: baseline + scannerConfidenceBump + 0.0001,
		},
		{
			name:    "tagged LLM (dot in category) gets +0.05",
			comment: model.LlmComment{Category: "security.hardcoded-secret", Severity: "high"},
			wantMin: 0.90 + llmTagConfidenceBump - 0.0001,
			wantMax: 0.90 + llmTagConfidenceBump + 0.0001,
		},
		{
			name:    "scanner source wins over tag (scanner path is taken first, no double-bump)",
			comment: model.LlmComment{Category: "security.hardcoded-secret", Severity: "high", Source: "scanner:semgrep"},
			wantMin: 0.90 + scannerConfidenceBump - 0.0001,
			wantMax: 0.90 + scannerConfidenceBump + 0.0001,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScoreOne(tt.comment, pol).Score.Confidence
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("confidence: got %.4f, want in [%.4f, %.4f]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestScoreConfidenceClampsAtOne(t *testing.T) {
	pol := Policy{
		Categories: map[string]PolicyEntry{
			"cve": {
				Impact:          0.9,
				ConfidenceFloor: 0.98,
				SeverityMap:     map[string]Severity{"default": SeverityHigh},
			},
		},
		Defaults: PolicyEntry{
			Impact:          0.4,
			ConfidenceFloor: 0.5,
			SeverityMap:     map[string]Severity{"default": SeverityMedium},
		},
	}
	c := model.LlmComment{Category: "cve", Source: "scanner:govulncheck"}
	got := ScoreOne(c, pol).Score.Confidence
	if got > 1.0 || got < 0.98 {
		t.Errorf("confidence must clamp at 1.0 and be >= floor, got %.4f", got)
	}
}

func TestScoreImpactVerbatim(t *testing.T) {
	pol := testPolicy()
	got := ScoreOne(model.LlmComment{Category: "security.hardcoded-secret", Severity: "high"}, pol)
	if got.Score.Impact != 0.95 {
		t.Errorf("impact must equal policy entry value, got %.4f", got.Score.Impact)
	}
}

func TestScoreRationaleNamesMatchedKey(t *testing.T) {
	pol := testPolicy()
	tests := []struct {
		category string
		want     string
	}{
		{"security.hardcoded-secret", "security.hardcoded-secret"},
		{"security.missing-auth", "security"}, // prefix fallback
		{"totally-unknown", "defaults"},
		{"", "defaults"},
	}
	for _, tt := range tests {
		t.Run(tt.category, func(t *testing.T) {
			got := ScoreOne(model.LlmComment{Category: tt.category}, pol).Score.Rationale
			if got != tt.want {
				t.Errorf("rationale: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestScoreAllPreservesOrder(t *testing.T) {
	pol := testPolicy()
	in := []model.LlmComment{
		{Path: "a", Category: "security"},
		{Path: "b", Category: "maintainability.naming"},
		{Path: "c", Category: "security.hardcoded-secret"},
	}
	got := ScoreAll(pol, in)
	if len(got) != 3 {
		t.Fatalf("len=%d", len(got))
	}
	for i, sc := range got {
		if sc.Path != in[i].Path {
			t.Errorf("order broken at %d: got %q want %q", i, sc.Path, in[i].Path)
		}
	}
}

func TestParseSeverity(t *testing.T) {
	cases := []struct {
		in   string
		want Severity
		ok   bool
	}{
		{"CRITICAL", SeverityCritical, true},
		{"critical", SeverityCritical, true},
		{"HiGh", SeverityHigh, true},
		{"medium", SeverityMedium, true},
		{"low", SeverityLow, true},
		{"suppress", SeveritySuppress, true},
		{"", "", false},
		{"blocker", "", false},
	}
	for _, tt := range cases {
		got, ok := ParseSeverity(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseSeverity(%q): got (%q,%v) want (%q,%v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRankOrder(t *testing.T) {
	order := []Severity{SeveritySuppress, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical}
	for i := 1; i < len(order); i++ {
		if Rank(order[i]) <= Rank(order[i-1]) {
			t.Errorf("rank not monotonic at %d: %s(%d) vs %s(%d)",
				i, order[i], Rank(order[i]), order[i-1], Rank(order[i-1]))
		}
	}
	if Rank("bogus") != -1 {
		t.Error("unknown severity should rank -1")
	}
}
