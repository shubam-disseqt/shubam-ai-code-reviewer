// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package metrics_dashboard reads zreview session JSONL logs and renders
// an offline HTML dashboard summarising cost, findings, and duration trends.
package metrics_dashboard

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RunMetrics mirrors cmd/zreview.Metrics plus a session timestamp for plotting.
type RunMetrics struct {
	SessionID        string    `json:"session_id"`
	Timestamp        time.Time `json:"timestamp"`
	DurationMs       int64     `json:"duration_ms"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalCostUSD     float64   `json:"total_cost_usd"`
	FilesReviewed    int       `json:"files_reviewed"`
	CommentsPosted   int       `json:"comments_posted"`
	ScannerFindings  int       `json:"scanner_findings"`
	CarriedFindings  int       `json:"carried_findings"`
	ResolvedFindings int       `json:"resolved_findings"`
	NewFindings      int       `json:"new_findings"`
	EffortScore      int       `json:"effort_score"`
}

// ParseDir walks dir for *.jsonl files. Empty/unreadable files are skipped
// rather than erroring — one stray file shouldn't blank the dashboard.
func ParseDir(dir string) ([]RunMetrics, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir %s: %w", dir, err)
	}
	var runs []RunMetrics
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if r, ok := parseFile(filepath.Join(dir, e.Name())); ok {
			runs = append(runs, r)
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Timestamp.Before(runs[j].Timestamp) })
	return runs, nil
}

var (
	metricsLineRE = regexp.MustCompile(`\[zreview\] metrics: (.+)$`)
	stageMetrics  = []byte(`"stage":"metrics"`)
	typeStart     = []byte(`"type":"session_start"`)
	typeEnd       = []byte(`"type":"session_end"`)
)

// parseFile folds all data from one session log into a RunMetrics. Priority:
// slog JSON stage=metrics > text "[zreview] metrics:" > session_end fallback.
// Timestamp: session_start > slog time > file mtime.
func parseFile(path string) (RunMetrics, bool) {
	f, err := os.Open(path) //nolint:gosec // path derived from ReadDir of a user flag
	if err != nil {
		return RunMetrics{}, false
	}
	defer f.Close()

	r := RunMetrics{SessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	haveData := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		switch {
		case len(line) == 0:
		case bytes.Contains(line, stageMetrics):
			if applyJSONMetrics(line, &r) {
				haveData = true
			}
		case bytes.Contains(line, typeStart):
			if ts := extractTimestamp(line); !ts.IsZero() {
				r.Timestamp = ts
				haveData = true
			}
		case bytes.Contains(line, typeEnd):
			applySessionEnd(line, &r)
			haveData = true
		default:
			if m := metricsLineRE.FindSubmatch(line); m != nil {
				applyTextMetrics(string(m[1]), &r)
				haveData = true
			}
		}
	}
	if !haveData {
		return RunMetrics{}, false
	}
	if r.Timestamp.IsZero() {
		if info, err := os.Stat(path); err == nil {
			r.Timestamp = info.ModTime()
		}
	}
	return r, true
}

// applyJSONMetrics unmarshals a stage=metrics slog record.
func applyJSONMetrics(line []byte, r *RunMetrics) bool {
	var m struct {
		Time             time.Time `json:"time"`
		DurationMs       int64     `json:"duration_ms"`
		PromptTokens     int64     `json:"prompt_tokens"`
		CompletionTokens int64     `json:"completion_tokens"`
		TotalCostUSD     float64   `json:"total_cost_usd"`
		FilesReviewed    int       `json:"files_reviewed"`
		CommentsPosted   int       `json:"comments_posted"`
		ScannerFindings  int       `json:"scanner_findings"`
		CarriedFindings  int       `json:"carried_findings"`
		ResolvedFindings int       `json:"resolved_findings"`
		NewFindings      int       `json:"new_findings"`
		EffortScore      int       `json:"effort_score"`
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return false
	}
	r.DurationMs, r.PromptTokens, r.CompletionTokens = m.DurationMs, m.PromptTokens, m.CompletionTokens
	r.TotalCostUSD, r.FilesReviewed, r.CommentsPosted = m.TotalCostUSD, m.FilesReviewed, m.CommentsPosted
	r.ScannerFindings, r.CarriedFindings, r.ResolvedFindings = m.ScannerFindings, m.CarriedFindings, m.ResolvedFindings
	r.NewFindings, r.EffortScore = m.NewFindings, m.EffortScore
	if !m.Time.IsZero() && r.Timestamp.IsZero() {
		r.Timestamp = m.Time
	}
	return true
}

func extractTimestamp(line []byte) time.Time {
	var m struct {
		Timestamp time.Time `json:"timestamp"`
	}
	_ = json.Unmarshal(line, &m)
	return m.Timestamp
}

// applySessionEnd fills files_reviewed + duration only when unset — never
// overrides an authoritative stage=metrics value.
func applySessionEnd(line []byte, r *RunMetrics) {
	var m struct {
		FilesReviewed   int     `json:"files_reviewed"`
		DurationSeconds float64 `json:"duration_seconds"`
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}
	if r.FilesReviewed == 0 {
		r.FilesReviewed = m.FilesReviewed
	}
	if r.DurationMs == 0 && m.DurationSeconds > 0 {
		r.DurationMs = int64(m.DurationSeconds * 1000)
	}
}

// applyTextMetrics parses "duration=1.2s files=8 tokens=in:X/out:Y cost=$Z
// findings=new:A/carried:B/resolved:C scanner=N comments=M". Missing fields
// stay zero; unknown fields are ignored.
func applyTextMetrics(payload string, r *RunMetrics) {
	for _, tok := range strings.Fields(payload) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		switch k {
		case "duration":
			r.DurationMs = parseDurationToken(v)
		case "files":
			r.FilesReviewed = atoi(v)
		case "tokens":
			for _, p := range strings.Split(v, "/") {
				sk, sv, _ := strings.Cut(p, ":")
				switch sk {
				case "in":
					r.PromptTokens = int64(atoi(sv))
				case "out":
					r.CompletionTokens = int64(atoi(sv))
				}
			}
		case "cost":
			r.TotalCostUSD = atof(strings.TrimPrefix(v, "$"))
		case "findings":
			for _, p := range strings.Split(v, "/") {
				sk, sv, _ := strings.Cut(p, ":")
				switch sk {
				case "new":
					r.NewFindings = atoi(sv)
				case "carried":
					r.CarriedFindings = atoi(sv)
				case "resolved":
					r.ResolvedFindings = atoi(sv)
				}
			}
		case "scanner":
			r.ScannerFindings = atoi(v)
		case "comments":
			r.CommentsPosted = atoi(v)
		}
	}
}

func parseDurationToken(v string) int64 {
	switch {
	case strings.HasSuffix(v, "ms"):
		return int64(atoi(strings.TrimSuffix(v, "ms")))
	case strings.HasSuffix(v, "s"):
		return int64(atof(strings.TrimSuffix(v, "s")) * 1000)
	}
	return 0
}

func atoi(s string) int     { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
func atof(s string) float64 { f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return f }
