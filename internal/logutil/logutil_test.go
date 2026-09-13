// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package logutil

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewDefaultsToLegacyTextOnUnknownFormat(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		wantPrefix  string
		wantJSONish bool
	}{
		{name: "text explicit", format: FormatText, wantPrefix: "[zreview] "},
		{name: "empty defaults to text", format: "", wantPrefix: "[zreview] "},
		{name: "unknown falls back to text", format: "yaml", wantPrefix: "[zreview] "},
		{name: "json switches formatter", format: FormatJSON, wantJSONish: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := New(&buf, tt.format, slog.LevelInfo)
			logger.Info("hello", "stage", "warmer", "pid", 42)

			out := buf.String()
			if tt.wantJSONish {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed); err != nil {
					t.Fatalf("expected valid JSON, got %q (err %v)", out, err)
				}
				if parsed["stage"] != "warmer" {
					t.Errorf("stage attr missing: %v", parsed)
				}
				return
			}
			if !strings.HasPrefix(out, tt.wantPrefix) {
				t.Errorf("prefix mismatch: got %q", out)
			}
			if !strings.Contains(out, "warmer: hello") {
				t.Errorf("stage prefix missing: %q", out)
			}
			if !strings.Contains(out, "pid=42") {
				t.Errorf("key=val missing: %q", out)
			}
		})
	}
}

func TestFromEnvReadsFormatAndLevel(t *testing.T) {
	tests := []struct {
		name       string
		envFormat  string
		envLevel   string
		emitLevel  slog.Level
		wantOutput bool
		wantJSON   bool
	}{
		{name: "defaults info visible", emitLevel: slog.LevelInfo, wantOutput: true},
		{name: "default filters debug", emitLevel: slog.LevelDebug, wantOutput: false},
		{name: "debug env allows debug", envLevel: "debug", emitLevel: slog.LevelDebug, wantOutput: true},
		{name: "warn env filters info", envLevel: "WARN", emitLevel: slog.LevelInfo, wantOutput: false},
		{name: "error env allows error", envLevel: "error", emitLevel: slog.LevelError, wantOutput: true},
		{name: "json format", envFormat: "json", emitLevel: slog.LevelInfo, wantOutput: true, wantJSON: true},
		{name: "garbage level falls back to info", envLevel: "purple", emitLevel: slog.LevelInfo, wantOutput: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvFormat, tt.envFormat)
			t.Setenv(EnvLevel, tt.envLevel)
			var buf bytes.Buffer
			logger := FromEnv(&buf)
			logger.Log(nil, tt.emitLevel, "msg", "stage", "test", "k", "v")

			got := buf.String()
			if tt.wantOutput && got == "" {
				t.Fatalf("expected output, got empty")
			}
			if !tt.wantOutput && got != "" {
				t.Errorf("expected filtered, got %q", got)
			}
			if tt.wantJSON && tt.wantOutput {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(strings.TrimSpace(got)), &parsed); err != nil {
					t.Errorf("JSON output not valid: %q", got)
				}
			}
		})
	}
}

func TestWithStageAttachesAttr(t *testing.T) {
	var buf bytes.Buffer
	base := New(&buf, FormatText, slog.LevelInfo)
	logger := WithStage(base, "carryover")
	logger.Info("counts", "new", 3)

	got := buf.String()
	if !strings.Contains(got, "carryover: counts") {
		t.Errorf("stage prefix missing: %q", got)
	}
	if !strings.Contains(got, "new=3") {
		t.Errorf("attr missing: %q", got)
	}
}

func TestWithStageNilLoggerReturnsNil(t *testing.T) {
	if got := WithStage(nil, "x"); got != nil {
		t.Errorf("expected nil for nil logger, got %v", got)
	}
}

func TestLegacyTextEscapesWhitespace(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, FormatText, slog.LevelInfo)
	logger.Info("row", "stage", "warmer", "path", "/tmp/warmer log.log")

	got := buf.String()
	if !strings.Contains(got, `path="/tmp/warmer log.log"`) {
		t.Errorf("expected quoted path, got %q", got)
	}
}

func TestLegacyTextLevelPrefix(t *testing.T) {
	tests := []struct {
		name     string
		emit     func(l *slog.Logger)
		contains string
	}{
		{"info has no level prefix", func(l *slog.Logger) { l.Info("m", "stage", "s") }, "[zreview] s: m"},
		{"warn shows level", func(l *slog.Logger) { l.Warn("m", "stage", "s") }, "[zreview] WARN s: m"},
		{"error shows level", func(l *slog.Logger) { l.Error("m", "stage", "s") }, "[zreview] ERROR s: m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := New(&buf, FormatText, slog.LevelDebug)
			tt.emit(logger)
			if !strings.Contains(buf.String(), tt.contains) {
				t.Errorf("got %q want contains %q", buf.String(), tt.contains)
			}
		})
	}
}

func TestLegacyTextStageAttrPreservedInJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, FormatJSON, slog.LevelInfo)
	WithStage(logger, "metrics").Info("summary", "files", 8, "cost", 0.42)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &parsed); err != nil {
		t.Fatalf("bad JSON: %v (%q)", err, buf.String())
	}
	if parsed["stage"] != "metrics" {
		t.Errorf("stage missing: %v", parsed)
	}
	if parsed["files"].(float64) != 8 {
		t.Errorf("files missing: %v", parsed)
	}
}

func TestNewNilWriterFallsBackToStderr(t *testing.T) {
	// Just confirm construction doesn't panic. We can't easily capture
	// stderr from the test without racing global state.
	logger := New(nil, FormatText, slog.LevelInfo)
	if logger == nil {
		t.Fatal("logger nil")
	}
}
