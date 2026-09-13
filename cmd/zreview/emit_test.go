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
	for _, ok := range []string{"stdout", "json", "github"} {
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
			if got := exitCodeForComments(tt.cms); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFormatGithubBodyIncludesSuggestion(t *testing.T) {
	c := model.LlmComment{
		Content:        "use := not =",
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
