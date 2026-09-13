// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestConvertGitleaks(t *testing.T) {
	data, err := os.ReadFile("testdata/gitleaks.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var raw []gitleaksFinding
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	got := convertGitleaks(raw, "")
	if len(got) != 2 {
		t.Fatalf("want 2 findings, got %d", len(got))
	}

	// First finding — AWS token.
	f := got[0]
	if f.Tool != "gitleaks" {
		t.Errorf("Tool: got %q, want gitleaks", f.Tool)
	}
	if f.RuleID != "aws-access-token" {
		t.Errorf("RuleID: got %q", f.RuleID)
	}
	if f.Kind != KindSecret {
		t.Errorf("Kind: got %q, want %q", f.Kind, KindSecret)
	}
	if f.Severity != SeverityHigh {
		t.Errorf("Severity: got %q, want HIGH", f.Severity)
	}
	if f.Path != "config/aws.py" {
		t.Errorf("Path: got %q", f.Path)
	}
	if f.Line != 5 {
		t.Errorf("Line: got %d", f.Line)
	}
	// The raw secret must never appear verbatim in Evidence.
	if strings.Contains(f.Evidence, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("Evidence leaked raw secret: %q", f.Evidence)
	}
	if !strings.Contains(f.Evidence, "REDACTED") {
		t.Errorf("Evidence should contain redaction marker: %q", f.Evidence)
	}
}

func TestRedactSecret(t *testing.T) {
	tests := []struct {
		name       string
		match      string
		secret     string
		wantHas    string
		wantNotHas string
	}{
		{"substitute", "key=hunter2more", "hunter2", "REDACTED", "hunter2"},
		{"empty secret", "raw match", "", "raw match", ""},
		{"empty match", "", "secret", "REDACTED", "secret"},
		{"disjoint", "envelope", "ghostword", "REDACTED", "ghostword"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactSecret(tt.match, tt.secret)
			if !strings.Contains(got, tt.wantHas) {
				t.Errorf("want %q in %q", tt.wantHas, got)
			}
			if tt.wantNotHas != "" && strings.Contains(got, tt.wantNotHas) {
				t.Errorf("must not leak %q, got %q", tt.wantNotHas, got)
			}
		})
	}
}

func TestGitleaksScannerName(t *testing.T) {
	s := &gitleaksScanner{}
	if s.Name() != "gitleaks" {
		t.Errorf("Name: got %q, want gitleaks", s.Name())
	}
}
