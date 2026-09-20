// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/logutil"
)

func TestEmitMetricsTextFormat(t *testing.T) {
	tests := []struct {
		name   string
		m      Metrics
		wantIn []string
	}{
		{
			name: "populated review",
			m: Metrics{
				DurationMs:       42300,
				PromptTokens:     12345,
				CompletionTokens: 2345,
				TotalCostUSD:     0.42,
				FilesReviewed:    8,
				CommentsPosted:   6,
				ScannerFindings:  2,
				CarriedFindings:  3,
				ResolvedFindings: 1,
				NewFindings:      5,
			},
			wantIn: []string{
				"[sacr] metrics: ",
				"duration=42.3s",
				"files=8",
				"tokens=in:12345/out:2345",
				"cost=$0.42",
				"findings=new:5/carried:3/resolved:1",
				"scanner=2",
				"comments=6",
			},
		},
		{
			name: "zero values still emit",
			m:    Metrics{},
			wantIn: []string{
				"[sacr] metrics: ",
				"duration=0ms",
				"files=0",
				"tokens=in:0/out:0",
				"cost=$0.00",
			},
		},
		{
			name: "sub-second duration",
			m:    Metrics{DurationMs: 42},
			wantIn: []string{
				"duration=42ms",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			emitMetrics(logutil.New(&buf, logutil.FormatText, slog.LevelInfo), tt.m)
			got := buf.String()
			for _, want := range tt.wantIn {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q\nfull output: %s", want, got)
				}
			}
		})
	}
}

func TestEmitMetricsJSONFormat(t *testing.T) {
	m := Metrics{
		DurationMs:       1234,
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalCostUSD:     0.05,
		FilesReviewed:    3,
		CommentsPosted:   4,
		ScannerFindings:  1,
		CarriedFindings:  2,
		ResolvedFindings: 0,
		NewFindings:      4,
	}
	var buf bytes.Buffer
	emitMetrics(logutil.New(&buf, logutil.FormatJSON, slog.LevelInfo), m)

	line := strings.TrimSpace(buf.String())
	var parsed map[string]any
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, line)
	}
	// Every metric field must survive the round trip. JSON numbers land as
	// float64 in a map[string]any, so compare numerics as float64.
	tests := []struct {
		key  string
		want any
	}{
		{"stage", "metrics"},
		{"duration_ms", float64(1234)},
		{"files_reviewed", float64(3)},
		{"prompt_tokens", float64(100)},
		{"completion_tokens", float64(50)},
		{"total_cost_usd", 0.05},
		{"new_findings", float64(4)},
		{"carried_findings", float64(2)},
		{"resolved_findings", float64(0)},
		{"scanner_findings", float64(1)},
		{"comments_posted", float64(4)},
	}
	for _, tt := range tests {
		if got := parsed[tt.key]; got != tt.want {
			t.Errorf("%s: got %v (%T) want %v (%T)", tt.key, got, got, tt.want, tt.want)
		}
	}
}

func TestEmitMetricsNilLoggerIsNoop(t *testing.T) {
	// Guard against a partially-initialized pipeline calling Emit with nil.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on nil logger: %v", r)
		}
	}()
	emitMetrics(nil, Metrics{FilesReviewed: 1})
}

func TestFormatDurationMs(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "0ms"},
		{42, "42ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{42300, "42.3s"},
		{60000, "60.0s"},
	}
	for _, tt := range tests {
		if got := formatDurationMs(tt.ms); got != tt.want {
			t.Errorf("formatDurationMs(%d): got %q want %q", tt.ms, got, tt.want)
		}
	}
}
