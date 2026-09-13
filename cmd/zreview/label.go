// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
)

// labelerMaxTokens caps labeler output. The response is a small structured
// envelope — 1k is more than enough and keeps the cheap-tier bill flat.
const labelerMaxTokens = 1024

// RunLabeler calls the cheap tier to produce Labels for the kept diffs.
// Same best-effort semantics as RunSummarizer.
func RunLabeler(ctx context.Context, cheap llm.LLMClient, cheapModel string, kept []model.Diff) (model.Labels, error) {
	if len(kept) == 0 {
		return model.Labels{}, nil
	}
	tmpl, err := loadPrompt("labeler.md")
	if err != nil {
		return model.Labels{}, fmt.Errorf("labeler: prompt: %w", err)
	}
	prompt := strings.ReplaceAll(tmpl, "{{diffs}}", renderDiffsForAll(kept))
	req := buildCheapJSONRequest(cheapModel, prompt, labelerMaxTokens)
	resp, err := cheap.CompletionsWithCtx(ctx, req)
	if err != nil {
		return model.Labels{}, fmt.Errorf("labeler: call: %w", err)
	}
	var out model.Labels
	if err := parseCheapJSON(resp.Content(), &out); err != nil {
		return model.Labels{}, fmt.Errorf("labeler: parse: %w", err)
	}
	return out, nil
}
