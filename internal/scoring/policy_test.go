// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scoring

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
)

func TestLoadPolicyEmbeddedDefault(t *testing.T) {
	t.Setenv("ZREVIEW_SCORING_POLICY", "")
	// Point at a repoRoot that has no override; must return embedded.
	p, err := LoadPolicy(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Source() != "embedded" {
		t.Errorf("source: got %q want embedded", p.Source())
	}
	// Sanity: default entries should include a "default" key so lookup
	// always resolves.
	if _, ok := p.Defaults.SeverityMap["default"]; !ok {
		t.Error("embedded defaults must contain 'default' key")
	}
	// The four PDF §6 examples must be present in the shipped default.
	for _, key := range []string{
		"security.hardcoded-secret",
		"security.missing-auth",
		"bug.missing-error-wrap",
		"maintainability.naming",
	} {
		if _, ok := p.Categories[key]; !ok {
			t.Errorf("embedded policy missing PDF example category %q", key)
		}
	}
	// Scanner-producer buckets must also be present.
	for _, key := range []string{"secret", "sast", "cve"} {
		if _, ok := p.Categories[key]; !ok {
			t.Errorf("embedded policy missing scanner bucket %q", key)
		}
	}
}

func TestLoadPolicyRepoLocalOverride(t *testing.T) {
	t.Setenv("ZREVIEW_SCORING_POLICY", "")
	repo := t.TempDir()
	dir := filepath.Join(repo, ".zreview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	local := filepath.Join(dir, "scoring.yaml")
	yaml := `
categories:
  bug:
    impact: 0.99
    confidence_floor: 0.42
    severity_map: {default: LOW}
defaults:
  impact: 0.1
  confidence_floor: 0.1
  severity_map: {default: MEDIUM}
`
	if err := os.WriteFile(local, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, err := LoadPolicy(repo)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Source() != local {
		t.Errorf("source: got %q want %q", p.Source(), local)
	}
	if p.Categories["bug"].Impact != 0.99 {
		t.Errorf("override not applied: %+v", p.Categories["bug"])
	}
}

func TestLoadPolicyEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	yaml := `
categories:
  bug: {impact: 0.42, confidence_floor: 0.42, severity_map: {default: HIGH}}
defaults: {impact: 0.1, confidence_floor: 0.1, severity_map: {default: MEDIUM}}
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("ZREVIEW_SCORING_POLICY", path)

	// Even with a repo-local file present, env override must win.
	repo := t.TempDir()
	local := filepath.Join(repo, ".zreview")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(local, "scoring.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write local: %v", err)
	}

	p, err := LoadPolicy(repo)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if p.Source() != path {
		t.Errorf("env override should win, got source=%q", p.Source())
	}
}

func TestLoadPolicyEnvPathMissing(t *testing.T) {
	t.Setenv("ZREVIEW_SCORING_POLICY", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if _, err := LoadPolicy(""); err == nil {
		t.Fatal("expected error when env-configured file is absent")
	}
}

func TestLoadPolicyParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("::: not yaml :::"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("ZREVIEW_SCORING_POLICY", path)
	if _, err := LoadPolicy(""); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadPolicyMissingDefaultsRejects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-defaults.yaml")
	yaml := `categories: {}`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("ZREVIEW_SCORING_POLICY", path)
	if _, err := LoadPolicy(""); err == nil {
		t.Fatal("expected error on missing defaults")
	}
}

func TestLoadPolicyMissingDefaultKeyRejects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-default-key.yaml")
	yaml := `
defaults:
  impact: 0.1
  confidence_floor: 0.1
  severity_map: {high: HIGH}
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("ZREVIEW_SCORING_POLICY", path)
	if _, err := LoadPolicy(""); err == nil {
		t.Fatal("expected error on missing 'default' key in defaults.severity_map")
	}
}

// Integration: the shipped default policy must score the four PDF
// examples the way the spec says it does.
func TestEmbeddedPolicyPDFExamples(t *testing.T) {
	t.Setenv("ZREVIEW_SCORING_POLICY", "")
	p, err := LoadPolicy("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cases := []struct {
		name    string
		comment model.LlmComment
		wantSev Severity
		wantRat string
	}{
		{
			name:    "hardcoded credential → CRITICAL",
			comment: model.LlmComment{Category: "security.hardcoded-secret", Severity: "high"},
			wantSev: SeverityCritical,
			wantRat: "security.hardcoded-secret",
		},
		{
			name:    "missing authorization → HIGH",
			comment: model.LlmComment{Category: "security.missing-auth", Severity: "high"},
			wantSev: SeverityHigh,
			wantRat: "security.missing-auth",
		},
		{
			name:    "missing error wrapping → MEDIUM",
			comment: model.LlmComment{Category: "bug.missing-error-wrap", Severity: "medium"},
			wantSev: SeverityMedium,
			wantRat: "bug.missing-error-wrap",
		},
		{
			name:    "naming suggestion → SUPPRESS",
			comment: model.LlmComment{Category: "maintainability.naming", Severity: "low"},
			wantSev: SeveritySuppress,
			wantRat: "maintainability.naming",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := ScoreOne(tt.comment, p)
			if got.Score.Severity != tt.wantSev {
				t.Errorf("severity: got %q want %q", got.Score.Severity, tt.wantSev)
			}
			if got.Score.Rationale != tt.wantRat {
				t.Errorf("rationale: got %q want %q", got.Score.Rationale, tt.wantRat)
			}
		})
	}
}
