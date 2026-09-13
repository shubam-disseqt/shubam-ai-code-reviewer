// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// stubScanner implements scannerImpl for tests. Returns the fixed findings
// and err combo unconditionally.
type stubScanner struct {
	name     string
	findings []ScannerFinding
	err      error
}

func (s *stubScanner) Name() string { return s.name }
func (s *stubScanner) Run(_ context.Context, _ string, _ []string) ([]ScannerFinding, error) {
	return s.findings, s.err
}

func TestRunAggregatesAndSorts(t *testing.T) {
	logs := &captureLog{}
	got, err := Run(context.Background(), "/repo", nil, Options{
		Log: logs.Log,
		Scanners: []scannerImpl{
			&stubScanner{name: "b-tool", findings: []ScannerFinding{
				{Tool: "b-tool", Path: "z.go", Line: 1, RuleID: "r1"},
			}},
			&stubScanner{name: "a-tool", findings: []ScannerFinding{
				{Tool: "a-tool", Path: "y.go", Line: 5, RuleID: "r2"},
				{Tool: "a-tool", Path: "y.go", Line: 3, RuleID: "r3"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 findings, got %d", len(got))
	}
	// Deterministic order: sort by tool then path then line.
	wantOrder := []string{"a-tool", "a-tool", "b-tool"}
	for i, want := range wantOrder {
		if got[i].Tool != want {
			t.Errorf("findings[%d].Tool: got %q, want %q", i, got[i].Tool, want)
		}
	}
	if got[0].Line != 3 || got[1].Line != 5 {
		t.Errorf("expected line sort within tool: got %d,%d", got[0].Line, got[1].Line)
	}
	if len(logs.lines) != 0 {
		t.Errorf("unexpected log lines: %v", logs.lines)
	}
}

func TestRunSurvivesSingleScannerError(t *testing.T) {
	logs := &captureLog{}
	got, err := Run(context.Background(), "/repo", nil, Options{
		Log: logs.Log,
		Scanners: []scannerImpl{
			&stubScanner{name: "broken", err: errors.New("boom")},
			&stubScanner{name: "healthy", findings: []ScannerFinding{
				{Tool: "healthy", Path: "a.go", Line: 1, RuleID: "r"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Run should not surface non-ctx error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("healthy scanner findings dropped, got %d", len(got))
	}
	if got[0].Tool != "healthy" {
		t.Errorf("wrong finding: %+v", got[0])
	}
	// The broken scanner should have produced a log line.
	joined := strings.Join(logs.lines, "\n")
	if !strings.Contains(joined, "broken") || !strings.Contains(joined, "boom") {
		t.Errorf("expected error log for broken scanner, got: %s", joined)
	}
}

func TestRunHonorsDisabled(t *testing.T) {
	logs := &captureLog{}
	got, err := Run(context.Background(), "/repo", nil, Options{
		Log:      logs.Log,
		Disabled: map[string]struct{}{"noisy": {}},
		Scanners: []scannerImpl{
			&stubScanner{name: "noisy", findings: []ScannerFinding{
				{Tool: "noisy", Path: "n.go", Line: 1},
			}},
			&stubScanner{name: "quiet", findings: []ScannerFinding{
				{Tool: "quiet", Path: "q.go", Line: 1},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 1 || got[0].Tool != "quiet" {
		t.Fatalf("disabled scanner leaked results: %+v", got)
	}
	if !strings.Contains(strings.Join(logs.lines, "\n"), "skipping noisy") {
		t.Errorf("expected skipping-noisy log, got %v", logs.lines)
	}
}

func TestRunSurfacesContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, "/repo", nil, Options{
		Scanners: []scannerImpl{
			&stubScanner{name: "any", err: context.Canceled},
		},
		Log: func(string, ...any) {},
	})
	if err == nil {
		t.Fatal("expected context.Canceled surfaced")
	}
}

func TestParseDisabled(t *testing.T) {
	got := ParseDisabled(" gitleaks,semgrep GOVULNCHECK ")
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d: %v", len(got), got)
	}
	for _, want := range []string{"gitleaks", "semgrep", "govulncheck"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q", want)
		}
	}
	if ParseDisabled("") != nil {
		t.Errorf("empty spec should yield nil map")
	}
}

func TestTallyByTool(t *testing.T) {
	got := TallyByTool([]ScannerFinding{
		{Tool: "gitleaks"}, {Tool: "gitleaks"},
		{Tool: "semgrep"},
	})
	// Zero-count tools still show up so the log line is stable.
	for _, want := range []string{"gitleaks:2", "semgrep:1", "govulncheck:0"} {
		if !strings.Contains(got, want) {
			t.Errorf("TallyByTool missing %q: %s", want, got)
		}
	}
}

// captureLog is a tiny helper that stores log lines for assertions.
type captureLog struct {
	lines []string
}

func (c *captureLog) Log(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}
