// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package metrics_dashboard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseDir_MixedSources verifies the parser handles all three source
// shapes: a slog JSON metrics record, a text "[zreview] metrics:" line,
// and session_start/session_end fallback only.
func TestParseDir_MixedSources(t *testing.T) {
	dir := t.TempDir()

	// 1. JSON metrics record (slog output).
	jsonSession := `{"time":"2026-01-15T10:00:00Z","type":"session_start","timestamp":"2026-01-15T10:00:00Z","sessionId":"s1"}
{"time":"2026-01-15T10:00:42Z","stage":"metrics","duration_ms":42300,"prompt_tokens":12345,"completion_tokens":2345,"total_cost_usd":0.42,"files_reviewed":8,"new_findings":5,"carried_findings":3,"resolved_findings":1,"effort_score":7}
`
	writeFile(t, filepath.Join(dir, "session1.jsonl"), jsonSession)

	// 2. Text metrics line embedded in a JSONL that also has session boundaries.
	textSession := `{"type":"session_start","timestamp":"2026-01-16T09:00:00Z","sessionId":"s2"}
[zreview] metrics: duration=1.5s files=3 tokens=in:500/out:100 cost=$0.05 findings=new:2/carried:1/resolved:0 scanner=1 comments=2
{"type":"session_end","files_reviewed":3,"duration_seconds":1.5}
`
	writeFile(t, filepath.Join(dir, "session2.jsonl"), textSession)

	// 3. Only session boundary records — fallback path.
	fallback := `{"type":"session_start","timestamp":"2026-01-17T08:00:00Z","sessionId":"s3"}
{"type":"session_end","files_reviewed":2,"duration_seconds":10.0}
`
	writeFile(t, filepath.Join(dir, "session3.jsonl"), fallback)

	// 4. Empty / junk file must be skipped, not error.
	writeFile(t, filepath.Join(dir, "empty.jsonl"), "")
	writeFile(t, filepath.Join(dir, "not-a-session.txt"), "ignore me")

	runs, err := ParseDir(dir)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("want 3 runs, got %d", len(runs))
	}
	// Sorted ascending by timestamp.
	if runs[0].SessionID != "session1" || runs[2].SessionID != "session3" {
		t.Errorf("wrong sort order: %+v", []string{runs[0].SessionID, runs[1].SessionID, runs[2].SessionID})
	}
	// JSON path: full fidelity.
	if runs[0].TotalCostUSD != 0.42 || runs[0].NewFindings != 5 || runs[0].EffortScore != 7 {
		t.Errorf("json parse: %+v", runs[0])
	}
	// Text path: all fields land.
	if runs[1].DurationMs != 1500 || runs[1].PromptTokens != 500 || runs[1].NewFindings != 2 || runs[1].ScannerFindings != 1 {
		t.Errorf("text parse: %+v", runs[1])
	}
	// Fallback path: files_reviewed + duration only.
	if runs[2].FilesReviewed != 2 || runs[2].DurationMs != 10000 {
		t.Errorf("fallback parse: %+v", runs[2])
	}
	if runs[2].TotalCostUSD != 0 {
		t.Errorf("fallback should have zero cost, got %v", runs[2].TotalCostUSD)
	}
}

// TestBuild_Totals_And_Buckets checks the aggregator sums correctly and
// bucketing prefers effort_score, falling back to cost when absent.
func TestBuild_Totals_And_Buckets(t *testing.T) {
	runs := []RunMetrics{
		{TotalCostUSD: 0.05, DurationMs: 1000, NewFindings: 1, CarriedFindings: 2, EffortScore: 2}, // green (score)
		{TotalCostUSD: 0.50, DurationMs: 3000, NewFindings: 3, EffortScore: 5},                     // yellow (score)
		{TotalCostUSD: 2.00, DurationMs: 5000, NewFindings: 4, EffortScore: 9},                     // red (score)
		{TotalCostUSD: 0.01, DurationMs: 500, NewFindings: 0},                                      // green (cost fallback)
	}
	rep := Build(runs)
	if rep.Totals.Runs != 4 {
		t.Errorf("runs: %d", rep.Totals.Runs)
	}
	if rep.Totals.TotalCostUSD < 2.55 || rep.Totals.TotalCostUSD > 2.57 {
		t.Errorf("cost: %v", rep.Totals.TotalCostUSD)
	}
	if rep.Totals.TotalNewFindings != 8 {
		t.Errorf("new findings: %d", rep.Totals.TotalNewFindings)
	}
	if rep.Totals.AvgDurationMs != 2375 {
		t.Errorf("avg duration: %d", rep.Totals.AvgDurationMs)
	}
	if rep.TrafficBuckets != [3]int{2, 1, 1} {
		t.Errorf("traffic buckets: %v", rep.TrafficBuckets)
	}
}

// TestRender_ProducesSelfContainedHTML makes sure the template renders with
// no execute errors and includes the CDN chart lib plus injected data.
func TestRender_ProducesSelfContainedHTML(t *testing.T) {
	runs := []RunMetrics{{SessionID: "abc", TotalCostUSD: 0.10, FilesReviewed: 2, NewFindings: 1}}
	var buf bytes.Buffer
	if err := Render(&buf, Build(runs)); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"<title>zreview metrics</title>", "chart.umd.min.js", "new Chart(", `"total_cost_usd":0.1`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// TestRender_EmptyRuns still produces a valid page with the empty-state banner.
func TestRender_EmptyRuns(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, Build(nil)); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(buf.String(), "No sessions found") {
		t.Errorf("empty state missing")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
