// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestConvertSemgrep(t *testing.T) {
	data, err := os.ReadFile("testdata/semgrep.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var parsed semgrepOutput
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	got := convertSemgrep(parsed.Results)
	if len(got) != 3 {
		t.Fatalf("want 3 findings, got %d", len(got))
	}

	// severity mapping check on the first three severity levels
	wantSeverities := []Severity{SeverityHigh, SeverityMedium, SeverityInfo}
	for i, sev := range wantSeverities {
		if got[i].Severity != sev {
			t.Errorf("finding[%d].Severity: got %q, want %q", i, got[i].Severity, sev)
		}
		if got[i].Tool != "semgrep" {
			t.Errorf("finding[%d].Tool: got %q", i, got[i].Tool)
		}
		if got[i].Kind != KindSAST {
			t.Errorf("finding[%d].Kind: got %q, want SAST", i, got[i].Kind)
		}
	}

	// Message and path should survive the mapping.
	if !strings.Contains(got[0].Description, "subprocess") {
		t.Errorf("Description lost message: %q", got[0].Description)
	}
	if got[0].Path != "scripts/deploy.py" {
		t.Errorf("Path: got %q", got[0].Path)
	}
	if got[0].Line != 42 {
		t.Errorf("Line: got %d", got[0].Line)
	}
}

func TestMapSemgrepSeverity(t *testing.T) {
	tests := []struct {
		in   string
		want Severity
	}{
		{"ERROR", SeverityHigh},
		{"WARNING", SeverityMedium},
		{"INFO", SeverityInfo},
		{"error", SeverityHigh},
		{"", SeverityMedium},
		{"unknown", SeverityMedium},
	}
	for _, tt := range tests {
		if got := mapSemgrepSeverity(tt.in); got != tt.want {
			t.Errorf("mapSemgrepSeverity(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSemgrepScannerName(t *testing.T) {
	s := &semgrepScanner{}
	if s.Name() != "semgrep" {
		t.Errorf("Name: got %q, want semgrep", s.Name())
	}
}
