// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package overlap

import (
	"strings"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/gh"
)

func TestBuildOverlapPrompt_Shape(t *testing.T) {
	cur := PR{
		Number: 100,
		Title:  "add rate limiter",
		Body:   "adds a limiter",
		Paths:  []string{"a.go", "b.go"},
	}
	cands := []candidate{{
		Ref:    gh.OpenPRRef{Number: 200},
		FP:     fingerprint{Number: 200, Title: "similar limiter", Body: "does the same"},
		Shared: []string{"a.go"},
	}}
	req := buildOverlapPrompt(cur, cands)
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Errorf("roles = %v", req.Messages)
	}
	user := req.Messages[1].Content.(string)
	if !strings.Contains(user, "## PR under review — #100: add rate limiter") {
		t.Errorf("missing current header, got:\n%s", user)
	}
	if !strings.Contains(user, "### PR #200: similar limiter") {
		t.Errorf("missing candidate header, got:\n%s", user)
	}
	if !strings.Contains(user, "Files shared with the PR under review: a.go") {
		t.Errorf("missing shared line, got:\n%s", user)
	}
}

func TestBuildOverlapPrompt_NoSharedFallsBackToCandidateFiles(t *testing.T) {
	cur := PR{Number: 1, Title: "x", Paths: []string{"a.go"}}
	cands := []candidate{{
		Ref: gh.OpenPRRef{Number: 2},
		FP:  fingerprint{Number: 2, Title: "y", Paths: []string{"z.go"}},
	}}
	req := buildOverlapPrompt(cur, cands)
	user := req.Messages[1].Content.(string)
	if !strings.Contains(user, "Files shared with the PR under review: none") {
		t.Errorf("want 'none' shared, got:\n%s", user)
	}
	if !strings.Contains(user, "Its changed files: z.go") {
		t.Errorf("want candidate files listed, got:\n%s", user)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hi", 100); got != "hi" {
		t.Errorf("short passthrough, got %q", got)
	}
	long := strings.Repeat("x", 700)
	got := truncate(long, 600)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("want ellipsis, got %q", got[len(got)-5:])
	}
	if len([]rune(got)) != 601 {
		t.Errorf("rune-length = %d, want 601", len([]rune(got)))
	}
	if got := truncate("  spaced  ", 100); got != "spaced" {
		t.Errorf("want trim, got %q", got)
	}
}

func TestTakeStrings(t *testing.T) {
	if got := takeStrings([]string{"a", "b", "c"}, 2); len(got) != 2 {
		t.Errorf("len=%d, want 2", len(got))
	}
	if got := takeStrings([]string{"a"}, 5); len(got) != 1 {
		t.Errorf("len=%d, want 1", len(got))
	}
}
