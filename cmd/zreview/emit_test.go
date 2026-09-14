// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
	"github.com/shubam-disseqt/z-code-reviewer/internal/scoring"
)

func sampleComment() model.LlmComment {
	return model.LlmComment{
		Path:      "foo.go",
		StartLine: 10,
		EndLine:   12,
		Content:   "avoid mutation",
		Severity:  "medium",
		Category:  "bug",
	}
}

func TestEmitStdoutRendersFinding(t *testing.T) {
	var buf bytes.Buffer
	err := emit(context.Background(), emitConfig{
		Format:   formatStdout,
		Comments: []model.LlmComment{sampleComment()},
		Stdout:   &buf,
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"foo.go:10-12", "medium", "bug", "avoid mutation"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q\n---\n%s", want, out)
		}
	}
}

func TestEmitStdoutNoFindings(t *testing.T) {
	var buf bytes.Buffer
	if err := emit(context.Background(), emitConfig{Format: formatStdout, Stdout: &buf}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(buf.String(), "No review findings") {
		t.Errorf("expected empty message, got %q", buf.String())
	}
}

func TestEmitJSONWritesEnvelope(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "res.json")
	err := emit(context.Background(), emitConfig{
		Format:    formatJSON,
		Output:    out,
		SessionID: "abc",
		Comments:  []model.LlmComment{sampleComment()},
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var res emitResult
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.SessionID != "abc" {
		t.Errorf("session id: got %q", res.SessionID)
	}
	if len(res.Comments) != 1 || res.Comments[0].Path != "foo.go" {
		t.Errorf("comments: %+v", res.Comments)
	}
}

func TestEmitJSONToStdout(t *testing.T) {
	var buf bytes.Buffer
	err := emit(context.Background(), emitConfig{
		Format:   formatJSON,
		Output:   "-",
		Comments: []model.LlmComment{sampleComment()},
		Stdout:   &buf,
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	var res emitResult
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v (out=%s)", err, buf.String())
	}
	if len(res.Comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(res.Comments))
	}
}

func TestEmitGithubWithoutClientErrors(t *testing.T) {
	err := emit(context.Background(), emitConfig{
		Format:   formatGithub,
		PRNumber: 42,
		Owner:    "o",
		Repo:     "r",
	})
	if err == nil {
		t.Fatal("expected error without gh client")
	}
	if !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Errorf("wrong err: %v", err)
	}
}

func TestValidFormat(t *testing.T) {
	for _, ok := range []string{"stdout", "json", "github", "sarif"} {
		if !validFormat(ok) {
			t.Errorf("format %q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "yaml", "toml"} {
		if validFormat(bad) {
			t.Errorf("format %q should be invalid", bad)
		}
	}
}

func TestExitCodeForBlockerFindings(t *testing.T) {
	tests := []struct {
		name string
		cms  []model.LlmComment
		want int
	}{
		{"no findings", nil, 0},
		{"only low", []model.LlmComment{{Severity: "low"}}, 0},
		{"critical bumps", []model.LlmComment{{Severity: "critical"}}, 3},
		{"blocker bumps", []model.LlmComment{{Severity: "blocker"}}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCodeForComments(tt.cms, nil, false); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEmitJSONAttachesScoreFields(t *testing.T) {
	var buf bytes.Buffer
	c := sampleComment()
	scores := map[string]scoring.Score{
		commentKey(c): {
			Severity:   scoring.SeverityHigh,
			Confidence: 0.87,
			Impact:     0.55,
			Rationale:  "bug",
		},
	}
	err := emit(context.Background(), emitConfig{
		Format:   formatJSON,
		Output:   "-",
		Comments: []model.LlmComment{c},
		Scores:   scores,
		Stdout:   &buf,
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// Assert the JSON envelope carries severity/confidence/impact/rationale
	// as top-level fields on each comment.
	var res struct {
		Comments []map[string]any `json:"comments"`
	}
	if err := json.Unmarshal(buf.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v (out=%s)", err, buf.String())
	}
	if len(res.Comments) != 1 {
		t.Fatalf("want 1 comment, got %d", len(res.Comments))
	}
	got := res.Comments[0]
	if got["severity"] != "HIGH" {
		t.Errorf("severity: got %v want HIGH", got["severity"])
	}
	if got["confidence"].(float64) != 0.87 {
		t.Errorf("confidence: got %v", got["confidence"])
	}
	if got["impact"].(float64) != 0.55 {
		t.Errorf("impact: got %v", got["impact"])
	}
	if got["rationale"] != "bug" {
		t.Errorf("rationale: got %v", got["rationale"])
	}
}

func TestEmitSARIF_WritesFileWithScannerComments(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "res.sarif")
	// One scanner-sourced comment (SARIF-bound) and one LLM comment (skipped).
	comments := []model.LlmComment{
		{Path: "a.go", StartLine: 1, EndLine: 1, Content: "hardcoded key",
			Source: "scanner:gitleaks", Category: "aws-key", Severity: "high"},
		{Path: "b.go", StartLine: 5, EndLine: 5, Content: "avoid mutation",
			Category: "bug", Severity: "medium"},
	}
	err := emit(context.Background(), emitConfig{
		Format:   formatSARIF,
		Output:   out,
		Comments: comments,
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	str := string(data)
	if !strings.Contains(str, "\"version\": \"2.1.0\"") {
		t.Errorf("missing SARIF version:\n%s", str)
	}
	if !strings.Contains(str, "scanner:gitleaks/aws-key") {
		t.Errorf("scanner rule ID missing:\n%s", str)
	}
	if strings.Contains(str, "avoid mutation") {
		t.Errorf("LLM comment should not appear in SARIF:\n%s", str)
	}
}

func TestEmitSARIF_StdoutWhenDashOutput(t *testing.T) {
	var buf bytes.Buffer
	err := emit(context.Background(), emitConfig{
		Format:   formatSARIF,
		Output:   "-",
		Comments: nil,
		Stdout:   &buf,
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(buf.String(), "\"results\": []") {
		t.Errorf("empty-set SARIF missing results:[]:\n%s", buf.String())
	}
}

func TestIsScannerSource(t *testing.T) {
	cases := map[string]bool{
		"":                 false,
		"llm":              false,
		"scanner:gitleaks": true,
		"scanner:semgrep":  true,
		"scanner":          false, // no colon → not a Phase-14 tag
		"scanner:x:y":      true,
	}
	for in, want := range cases {
		if got := isScannerSource(in); got != want {
			t.Errorf("isScannerSource(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFormatGithubBodyIncludesSuggestion(t *testing.T) {
	c := model.LlmComment{
		Content:        "use := not =",
		ExistingCode:   "x = 1",
		SuggestionCode: "x := 1",
		Severity:       "high",
		Category:       "bug",
	}
	body := formatGithubBody(c)
	for _, want := range []string{"high", "bug", "use := not =", "```suggestion", "x := 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}
}

func TestFormatGithubBodyContentOnly(t *testing.T) {
	// Comment with only content — no suggestion block, no nit prefix.
	c := model.LlmComment{
		Content:  "consider refactoring",
		Severity: "medium",
		Category: "maintainability",
	}
	body := formatGithubBody(c)
	if strings.Contains(body, "```suggestion") {
		t.Errorf("unexpected suggestion block: %s", body)
	}
	if strings.Contains(body, "**[nit]**") {
		t.Errorf("unexpected nit prefix on medium severity: %s", body)
	}
	if !strings.Contains(body, "consider refactoring") {
		t.Errorf("missing content: %s", body)
	}
}

func TestFormatGithubBodyNitPrefix(t *testing.T) {
	cases := []struct {
		name     string
		severity string
		category string
		wantNit  bool
	}{
		{"low style", "low", "style", true},
		{"low maintainability", "low", "maintainability", true},
		{"low test", "low", "test", true},
		{"low documentation", "low", "documentation", true},
		{"low bug is not a nit", "low", "bug", false},
		{"medium style is not a nit", "medium", "style", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			body := formatGithubBody(model.LlmComment{
				Content:  "x",
				Severity: tt.severity,
				Category: tt.category,
			})
			got := strings.Contains(body, "**[nit]**")
			if got != tt.wantNit {
				t.Errorf("nit prefix: got=%v want=%v body=%q", got, tt.wantNit, body)
			}
		})
	}
}

func TestFormatGithubBodyDropsSuggestionWithBackticks(t *testing.T) {
	// A suggestion containing ``` would break our fence — drop the block,
	// keep the content.
	c := model.LlmComment{
		Content:        "escape the fence",
		ExistingCode:   "old",
		SuggestionCode: "before ``` after",
		Severity:       "low",
		Category:       "style",
	}
	body := formatGithubBody(c)
	if strings.Contains(body, "```suggestion") {
		t.Errorf("suggestion block should be dropped when code contains backticks: %s", body)
	}
	if !strings.Contains(body, "escape the fence") {
		t.Errorf("content still expected in body: %s", body)
	}
}

func TestFormatGithubBodySkipsSuggestionWithoutExisting(t *testing.T) {
	// SuggestionCode without ExistingCode → no fence (anchor missing).
	c := model.LlmComment{
		Content:        "no anchor",
		SuggestionCode: "x := 1",
		Severity:       "low",
		Category:       "style",
	}
	body := formatGithubBody(c)
	if strings.Contains(body, "```suggestion") {
		t.Errorf("suggestion block should require ExistingCode: %s", body)
	}
}
