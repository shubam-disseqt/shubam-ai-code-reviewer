// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

func TestRunLabelerParsesStrictJSON(t *testing.T) {
	stub := &stubLLM{response: `{
      "pr_type": "feat",
      "domains": ["auth", "billing"],
      "risk_tag": "risk/medium",
      "ownership_hints": ["backend"]
    }`}
	got, err := RunLabeler(context.Background(), stub, "cheap", []model.Diff{{NewPath: "auth.go", Diff: "d"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.PRType != "feat" {
		t.Errorf("pr_type: %q", got.PRType)
	}
	if len(got.Domains) != 2 {
		t.Errorf("domains: %+v", got.Domains)
	}
	if got.RiskTag != "risk/medium" {
		t.Errorf("risk_tag: %q", got.RiskTag)
	}
	if len(got.OwnershipHints) != 1 || got.OwnershipHints[0] != "backend" {
		t.Errorf("ownership_hints: %+v", got.OwnershipHints)
	}
}

func TestRunLabelerEmptyDiffsShortCircuits(t *testing.T) {
	stub := &stubLLM{err: errors.New("should not be called")}
	got, err := RunLabeler(context.Background(), stub, "cheap", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.PRType != "" {
		t.Errorf("expected zero Labels, got %+v", got)
	}
}

func TestRunLabelerLLMErrorReturnsZero(t *testing.T) {
	stub := &stubLLM{err: errors.New("boom")}
	got, err := RunLabeler(context.Background(), stub, "cheap", []model.Diff{{NewPath: "a.go", Diff: "d"}})
	if err == nil {
		t.Fatal("expected err")
	}
	if got.PRType != "" {
		t.Errorf("expected zero Labels, got %+v", got)
	}
}

func TestRunLabelerParseFailureReturnsErr(t *testing.T) {
	stub := &stubLLM{response: "definitely not json"}
	got, err := RunLabeler(context.Background(), stub, "cheap", []model.Diff{{NewPath: "a.go", Diff: "d"}})
	if err == nil {
		t.Fatal("expected parse err")
	}
	if got.PRType != "" {
		t.Errorf("expected zero Labels, got %+v", got)
	}
}
