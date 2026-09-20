// SPDX-License-Identifier: Apache-2.0
// Portions Copyright 2026 shubam-ai-code-reviewer contributors
// Original abstraction distilled from alibaba/open-code-review internal/config/template.
//
// The upstream config/template package parses a YAML file into a much richer
// shape (prompts, tool defs, retry rules). The loop only needs the numeric
// limits and the memory-compression conversation template; declaring the
// smaller surface here keeps llmloop compilable ahead of a real config port,
// and the future prompts/config package can adopt the same field names.

package llmloop

// Template carries the per-run limits and prompt conversations the loop
// consults. Callers construct one from wherever their configuration is loaded
// (env vars, YAML file, hard-coded defaults) and pass it in via Deps.Template.
type Template struct {
	// MaxTokens is the model's total context window. The 60% / 80% compression
	// thresholds are computed from this.
	MaxTokens int
	// MaxCompletionTokens caps the completion (max_tokens on the request). Zero
	// means the caller does not want to cap it — CompletionTokenLimit falls
	// back to MaxTokens in that case.
	MaxCompletionTokens int
	// MaxToolRequestTimes is the round budget for one MAIN_TASK conversation.
	// The upstream default is 100.
	MaxToolRequestTimes int
	// MemoryCompressionTask is the conversation template used when the loop
	// triggers memory compression. An empty Messages list disables compression.
	MemoryCompressionTask LlmConversation
	// ReLocationTask is the conversation template used when a comment's
	// ExistingCode fails deterministic positioning. An empty Messages list
	// disables the LLM re-locate fallback.
	ReLocationTask LlmConversation
}

// ReLocationPrompts flattens ReLocationTask.Messages into the system+user
// pair BuildReLocationMessages consumes. Roles other than "system"/"user"
// are ignored; empty when either message is absent.
func (t Template) ReLocationPrompts() (system, user string) {
	for _, m := range t.ReLocationTask.Messages {
		switch m.Role {
		case "system":
			system = m.Content
		case "user":
			user = m.Content
		}
	}
	return
}

// LlmConversation is a rendered chat conversation, one ChatMessage per role.
type LlmConversation struct {
	Messages []ChatMessage
}

// ChatMessage is a single message in a prompt template. The Content string
// may embed template variables like {{context}} which the loop substitutes
// before dispatch.
type ChatMessage struct {
	Role    string
	Content string
}

// CompletionTokenLimit returns the max_tokens value to send with a chat
// request. If MaxCompletionTokens is set it is returned as-is; otherwise
// MaxTokens is used so the loop still bounds the completion.
func (t Template) CompletionTokenLimit() int {
	if t.MaxCompletionTokens > 0 {
		return t.MaxCompletionTokens
	}
	return t.MaxTokens
}
