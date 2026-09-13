// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
)

// summarizerMaxTokens caps the summarizer's output size. Same order-of-magnitude
// as the indexer's summarize call: this is prose + a small JSON envelope, so
// 4k is plenty and keeps a runaway model from burning cheap-tier budget.
const summarizerMaxTokens = 4096

// RunSummarizer calls the cheap tier to produce a Summary from the kept
// diffs. Best-effort: any error returns a zero Summary + err, and the caller
// logs and continues — the review pipeline never blocks on this.
func RunSummarizer(ctx context.Context, cheap llm.LLMClient, cheapModel string, kept []model.Diff) (model.Summary, error) {
	if len(kept) == 0 {
		return model.Summary{}, nil
	}
	tmpl, err := loadPrompt("summarizer.md")
	if err != nil {
		return model.Summary{}, fmt.Errorf("summarizer: prompt: %w", err)
	}
	prompt := strings.ReplaceAll(tmpl, "{{diffs}}", renderDiffsForAll(kept))
	req := buildCheapJSONRequest(cheapModel, prompt, summarizerMaxTokens)
	resp, err := cheap.CompletionsWithCtx(ctx, req)
	if err != nil {
		return model.Summary{}, fmt.Errorf("summarizer: call: %w", err)
	}
	var out model.Summary
	if err := parseCheapJSON(resp.Content(), &out); err != nil {
		return model.Summary{}, fmt.Errorf("summarizer: parse: %w", err)
	}
	return out, nil
}

// renderDiffsForAll concatenates every kept diff into the <file>…</file> block
// shape the main-task prompt also uses. Cheap tier reads the whole PR at once.
func renderDiffsForAll(diffs []model.Diff) string {
	var b strings.Builder
	for _, d := range diffs {
		b.WriteString(renderDiffsForFile(d))
	}
	return b.String()
}

// buildCheapJSONRequest is the shared shape for both cheap-tier structured
// calls. Temperature 0 so the JSON is stable across retries, and a modest
// max_tokens because both outputs are small envelopes.
func buildCheapJSONRequest(model, prompt string, maxTokens int) llm.ChatRequest {
	temp := 0.0
	return llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			llm.NewTextMessage("system", prompt),
			llm.NewTextMessage("user", "Produce the JSON described above."),
		},
		Temperature: &temp,
		MaxTokens:   maxTokens,
	}
}

// parseCheapJSON is defensive: models sometimes wrap the payload in
// ```json fences, sprinkle <think> blocks, or emit leading/trailing prose.
// We strip fences and thinking, then locate the outermost {...} object and
// unmarshal. Ceiling: only handles a single JSON object per response — a
// bare array would need a different helper. ponytail: object-only extractor,
// upgrade if a caller ever needs a top-level array.
func parseCheapJSON(raw string, dst any) error {
	text := stripCheapFences(stripCheapThinking(strings.TrimSpace(raw)))
	obj := extractOuterObject(text)
	if obj == "" {
		return fmt.Errorf("no JSON object in response")
	}
	if err := json.Unmarshal([]byte(obj), dst); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}

// stripCheapFences drops a leading ```json / ``` fence and trailing ```.
func stripCheapFences(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	if nl := strings.IndexByte(text, '\n'); nl >= 0 {
		text = text[nl+1:]
	}
	text = strings.TrimRight(text, " \t\r\n")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

// stripCheapThinking removes <think>…</think> segments, matching the same
// leakage pattern parseSummarizeResponse handles in internal/index.
func stripCheapThinking(raw string) string {
	out := raw
	for {
		start := strings.Index(out, "<think>")
		if start < 0 {
			return out
		}
		end := strings.Index(out[start:], "</think>")
		if end < 0 {
			return out[:start]
		}
		out = out[:start] + out[start+end+len("</think>"):]
	}
}

// extractOuterObject returns the substring from the first "{" to the matching
// "}", accounting for nested braces and JSON string literals. Returns "" if
// no balanced object is found. This is what makes the parser tolerant of
// prose bracketing the JSON.
func extractOuterObject(text string) string {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}
