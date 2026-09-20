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

func TestSelectPresets(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  []string // preset names, in stable order
	}{
		{"empty", nil, nil},
		{"go only skips presets", []string{"cmd/foo/main.go", "internal/bar/x.go"}, nil},
		{"js diff", []string{"src/app.js", "README.md"}, []string{"javascript"}},
		{"tsx diff", []string{"web/Button.tsx"}, []string{"javascript"}},
		{"py diff", []string{"scripts/deploy.py"}, []string{"python"}},
		{"rb diff", []string{"app/models/user.rb"}, []string{"ruby"}},
		{"gemfile diff", []string{"Gemfile"}, []string{"ruby"}},
		{"mixed diff", []string{"src/app.ts", "scripts/deploy.py", "app/x.rb"},
			[]string{"javascript", "python", "ruby"}},
		{"unknown ext skips", []string{"docs/spec.md", "config.toml"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectPresets(tt.paths)
			if len(got) != len(tt.want) {
				t.Fatalf("presets: got %d (%v), want %d (%v)", len(got), presetNames(got), len(tt.want), tt.want)
			}
			for i, p := range got {
				if p.name != tt.want[i] {
					t.Errorf("preset[%d]: got %q, want %q", i, p.name, tt.want[i])
				}
			}
		})
	}
}

func presetNames(ps []preset) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.name
	}
	return out
}

func TestResolveSemgrepConfigs(t *testing.T) {
	t.Run("override env wins", func(t *testing.T) {
		t.Setenv("SACR_SEMGREP_CONFIG", "p/owasp-top-ten")
		t.Setenv("SACR_DISABLE_SEMGREP_PRESETS", "1") // ignored when override set
		cfgs, cleanup, err := resolveSemgrepConfigs([]string{"a.py"})
		defer cleanup()
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if len(cfgs) != 1 || cfgs[0] != "p/owasp-top-ten" {
			t.Errorf("got %v, want [p/owasp-top-ten]", cfgs)
		}
	})

	t.Run("disable flag returns nil", func(t *testing.T) {
		t.Setenv("SACR_SEMGREP_CONFIG", "")
		t.Setenv("SACR_DISABLE_SEMGREP_PRESETS", "1")
		cfgs, cleanup, err := resolveSemgrepConfigs([]string{"a.py", "b.js"})
		defer cleanup()
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if len(cfgs) != 0 {
			t.Errorf("expected no configs when disabled, got %v", cfgs)
		}
	})

	t.Run("presets materialised for py+js diff", func(t *testing.T) {
		t.Setenv("SACR_SEMGREP_CONFIG", "")
		t.Setenv("SACR_DISABLE_SEMGREP_PRESETS", "")
		cfgs, cleanup, err := resolveSemgrepConfigs([]string{"scripts/deploy.py", "src/app.js"})
		defer cleanup()
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if len(cfgs) != 2 {
			t.Fatalf("want 2 configs, got %d (%v)", len(cfgs), cfgs)
		}
		for _, path := range cfgs {
			if _, err := os.Stat(path); err != nil {
				t.Errorf("preset file %s not written: %v", path, err)
			}
			if !strings.HasSuffix(path, ".yml") {
				t.Errorf("preset path %q missing .yml suffix", path)
			}
		}
	})

	t.Run("go-only diff produces no configs", func(t *testing.T) {
		t.Setenv("SACR_SEMGREP_CONFIG", "")
		t.Setenv("SACR_DISABLE_SEMGREP_PRESETS", "")
		cfgs, cleanup, err := resolveSemgrepConfigs([]string{"main.go", "internal/x/y.go"})
		defer cleanup()
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if len(cfgs) != 0 {
			t.Errorf("expected no configs for Go-only diff, got %v", cfgs)
		}
	})
}
