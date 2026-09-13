// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
)

// stubLLM is a canned-response LLM client for cheap-tier tests.
type stubLLM struct {
	response string
	err      error
	lastReq  llm.ChatRequest
}

func (s *stubLLM) CompletionsWithCtx(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	content := s.response
	return &llm.ChatResponse{
		Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant", Content: &content}}},
	}, nil
}

func TestRunSummarizerParsesStrictJSON(t *testing.T) {
	stub := &stubLLM{response: `{
      "walkthrough": "Adds a login guard.",
      "change_groups": [{"title": "auth", "files": ["auth.go"], "summary": "New guard"}],
      "testing_notes": "Try /login with a stale token.",
      "risk": "low: isolated auth change"
    }`}
	got, err := RunSummarizer(context.Background(), stub, "cheap-model", []model.Diff{{NewPath: "auth.go", Diff: "diff"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Walkthrough != "Adds a login guard." {
		t.Errorf("walkthrough: %q", got.Walkthrough)
	}
	if len(got.ChangeGroups) != 1 || got.ChangeGroups[0].Title != "auth" {
		t.Errorf("change_groups: %+v", got.ChangeGroups)
	}
	if !strings.HasPrefix(got.Risk, "low") {
		t.Errorf("risk: %q", got.Risk)
	}
	// Prompt should carry the diff into the system message.
	if len(stub.lastReq.Messages) == 0 {
		t.Fatalf("no messages sent")
	}
	sys := stub.lastReq.Messages[0].ExtractText()
	if !strings.Contains(sys, "auth.go") {
		t.Errorf("system prompt missing filename: %q", sys)
	}
}

func TestRunSummarizerStripsCodeFencesAndProse(t *testing.T) {
	// Real-world defect surface: models sometimes wrap JSON in ```json fences
	// or prepend "Here is the summary:" prose. Both must round-trip.
	stub := &stubLLM{response: "Here is the summary:\n```json\n" +
		`{"walkthrough": "x", "risk": "high: dangerous"}` +
		"\n```\ntrailing text"}
	got, err := RunSummarizer(context.Background(), stub, "cheap", []model.Diff{{NewPath: "a.go", Diff: "d"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Walkthrough != "x" || !strings.HasPrefix(got.Risk, "high") {
		t.Errorf("got %+v", got)
	}
}

func TestRunSummarizerEmptyDiffsShortCircuits(t *testing.T) {
	// A stub that would explode if called — proves the early return.
	stub := &stubLLM{err: errors.New("should not be called")}
	got, err := RunSummarizer(context.Background(), stub, "cheap", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Walkthrough != "" {
		t.Errorf("expected zero Summary, got %+v", got)
	}
}

func TestRunSummarizerLLMErrorReturnsZero(t *testing.T) {
	stub := &stubLLM{err: errors.New("boom")}
	got, err := RunSummarizer(context.Background(), stub, "cheap", []model.Diff{{NewPath: "a.go", Diff: "d"}})
	if err == nil {
		t.Fatal("expected err")
	}
	if got.Walkthrough != "" || len(got.ChangeGroups) != 0 {
		t.Errorf("expected zero Summary on err, got %+v", got)
	}
}

func TestRunSummarizerUnparseableJSONReturnsErr(t *testing.T) {
	stub := &stubLLM{response: "not JSON at all"}
	got, err := RunSummarizer(context.Background(), stub, "cheap", []model.Diff{{NewPath: "a.go", Diff: "d"}})
	if err == nil {
		t.Fatal("expected parse err")
	}
	if got.Walkthrough != "" {
		t.Errorf("expected zero Summary, got %+v", got)
	}
}

func TestParseCheapJSONHandlesThinkBlocks(t *testing.T) {
	raw := "<think>reasoning here</think>\n{\"pr_type\": \"fix\"}"
	var out model.Labels
	if err := parseCheapJSON(raw, &out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out.PRType != "fix" {
		t.Errorf("got %+v", out)
	}
}

func TestExtractOuterObjectBalancesBraces(t *testing.T) {
	// Nested braces inside a string literal must not confuse the extractor.
	in := `prefix {"a": {"b": "}"}, "c": 1} trailing`
	got := extractOuterObject(in)
	want := `{"a": {"b": "}"}, "c": 1}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractOuterObjectNoBraces(t *testing.T) {
	if got := extractOuterObject("no json here"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
