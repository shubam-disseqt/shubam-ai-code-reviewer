// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package index

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llm"
)

// dirsFakeLLM answers both prompts IndexRepo issues: the file-summarize
// prompt with a summarizeEnvelope, and the dir-summarize prompt with a
// dirSummaryResponse. Dispatch is on the user-role nudge ("Summarize the
// files above." vs "Summarize these directories.").
type dirsFakeLLM struct {
	calls    int32
	dirCalls int32
}

func (f *dirsFakeLLM) CompletionsWithCtx(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	atomic.AddInt32(&f.calls, 1)
	var sys, user string
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			sys = m.ExtractText()
		case "user":
			user = m.ExtractText()
		}
	}
	var body []byte
	if strings.Contains(user, "directories") {
		atomic.AddInt32(&f.dirCalls, 1)
		var out dirSummaryResponse
		for _, line := range strings.Split(sys, "\n") {
			if !strings.HasPrefix(line, "### ") {
				continue
			}
			display := strings.TrimPrefix(line, "### ")
			if paren := strings.Index(display, " ("); paren > 0 {
				display = display[:paren]
			}
			out.Directories = append(out.Directories, struct {
				Path    string `json:"path"`
				Summary string `json:"summary"`
			}{Path: display, Summary: "dir: " + display})
		}
		body, _ = json.Marshal(out)
	} else {
		// File summarize path — echo one entry per `### path` header.
		env := summarizeEnvelope{}
		for _, line := range strings.Split(sys, "\n") {
			if !strings.HasPrefix(line, "### ") {
				continue
			}
			p := strings.TrimPrefix(line, "### ")
			env.Files = append(env.Files, summarizeFileJSON{
				Path: p, Language: "go", Summary: "auto: " + p,
			})
		}
		body, _ = json.Marshal(env)
	}
	s := string(body)
	return &llm.ChatResponse{
		Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant", Content: &s}}},
	}, nil
}

func TestSummarizeDirectories_PopulatesRoot(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go":     strings.Repeat("package a\n// line\n", 100),
		"pkg/b.go": strings.Repeat("package pkg\n// line\n", 100),
	})
	store := openTestStore(t)
	fake := &dirsFakeLLM{}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test"})
	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	// After IndexRepo, both root ("") and "pkg" should have summaries.
	got, err := store.ListDirectorySummaries(context.Background(), []string{"", "pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 dir summaries, got %d: %+v", len(got), got)
	}
	seen := map[string]string{}
	for _, d := range got {
		seen[d.Path] = d.Summary
	}
	if !strings.Contains(seen[""], "root") && !strings.Contains(seen[""], "(root)") {
		t.Errorf("root summary missing: %q", seen[""])
	}
	if !strings.Contains(seen["pkg"], "pkg") {
		t.Errorf("pkg summary missing: %q", seen["pkg"])
	}
}

func TestSummarizeDirectories_SkippedOnNoChangeRerun(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go": strings.Repeat("package a\n// line\n", 100),
	})
	store := openTestStore(t)
	fake := &dirsFakeLLM{}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test"})
	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	first := atomic.LoadInt32(&fake.calls)
	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&fake.calls); got != first {
		t.Fatalf("expected 0 new LLM calls after unchanged re-run, got %d (was %d)", got-first, first)
	}
}
