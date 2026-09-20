// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 shubam-ai-code-reviewer contributors — adapted from alibaba/open-code-review
// Adapted from alibaba/open-code-review internal/diff/relocation.go

package diff

import (
	"context"
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// BuildReLocationMessages renders the re-location prompt for cm against d.
// Returns nil when userTemplate is empty, which the caller treats as
// "no re-location attempt": no request. systemPrompt is optional; when
// empty, only the user message is emitted.
func BuildReLocationMessages(cm *model.LlmComment, d *model.Diff, systemPrompt, userTemplate string) []llm.Message {
	if userTemplate == "" {
		return nil
	}
	userContent := userTemplate
	userContent = strings.ReplaceAll(userContent, "{diff}", d.Diff)
	userContent = strings.ReplaceAll(userContent, "{existing_code}", cm.ExistingCode)
	userContent = strings.ReplaceAll(userContent, "{suggestion_content}", cm.Content)
	if systemPrompt == "" {
		return []llm.Message{llm.NewTextMessage("user", userContent)}
	}
	return []llm.Message{
		llm.NewTextMessage("system", systemPrompt),
		llm.NewTextMessage("user", userContent),
	}
}

// ReLocateComment calls the LLM to regenerate a precise existing_code snippet
// when text-based matching fails, then retries ResolveComment with the new
// snippet. messages comes from BuildReLocationMessages. Response is nil when
// the request failed.
func ReLocateComment(
	ctx context.Context,
	cm *model.LlmComment,
	d *model.Diff,
	client llm.LLMClient,
	messages []llm.Message,
	modelName string,
	maxTokens int,
) (bool, *llm.ChatResponse) {
	if len(messages) == 0 {
		return false, nil
	}
	resp, err := client.CompletionsWithCtx(ctx, llm.ChatRequest{
		Model:     modelName,
		Messages:  messages,
		MaxTokens: maxTokens,
	})
	if err != nil {
		return false, nil
	}
	code := extractCodeBlock(resp.Content())
	if code == "" {
		return false, resp
	}
	original := cm.ExistingCode
	cm.ExistingCode = code
	if ResolveComment(cm, d) {
		return true, resp
	}
	cm.ExistingCode = original
	return false, resp
}

// extractCodeBlock extracts the content of the first fenced code block from text.
// Returns empty string if no code block is found.
func extractCodeBlock(text string) string {
	text = strings.TrimSpace(text)
	start := strings.Index(text, "```")
	if start < 0 {
		return ""
	}
	afterOpen := start + 3
	// Skip optional language tag on the opening fence line.
	if nl := strings.IndexByte(text[afterOpen:], '\n'); nl >= 0 {
		afterOpen += nl + 1
	} else {
		return ""
	}
	end := strings.Index(text[afterOpen:], "```")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(text[afterOpen : afterOpen+end])
}
