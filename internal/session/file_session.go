// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt
//
// Adapted from alibaba/open-code-review internal/session/history.go under
// Apache License 2.0. Modifications: dropped viewer, manifest coverage, and
// sealed-input identity; simplified to the interface declared by
// internal/llmloop.FileSession.

package session

import (
	"sync"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llmloop"
)

// FileSession is the per-taskKey append surface. RequestNo is scoped to this
// FileSession + taskType pair so records within one task stay numbered
// sequentially, matching OCR's shape.
type FileSession struct {
	mu          sync.Mutex
	session     *Session
	taskKey     string
	taskRecords map[llmloop.TaskType]int // taskType -> next requestNo - 1
}

// AppendTaskRecord writes an llm_request record and returns a TaskRecord bound
// to the same file session. The returned TaskRecord's write methods append
// further records (llm_response, llm_error, tool_call) to the shared session
// log.
func (fs *FileSession) AppendTaskRecord(taskType llmloop.TaskType, msgs []llm.Message) llmloop.TaskRecord {
	fs.mu.Lock()
	fs.taskRecords[taskType]++
	requestNo := fs.taskRecords[taskType]
	fs.mu.Unlock()

	s := fs.session
	s.mu.Lock()
	defer s.mu.Unlock()

	base := s.newBaseLocked(typeLLMRequest)
	rec := llmRequestRecord{
		baseRecord: base,
		FilePath:   fs.taskKey,
		TaskType:   string(taskType),
		RequestNo:  requestNo,
		Messages:   copyMessages(msgs),
	}
	s.commitLocked(base.UUID, rec)

	return &TaskRecord{
		fileSession: fs,
		taskType:    taskType,
		requestNo:   requestNo,
	}
}

// copyMessages returns a deep-ish copy of msgs so a caller mutating the slice
// later does not corrupt the persisted record. Only fields the log serializes
// need copying; Native is opaque provider state and is intentionally omitted
// from the JSONL (matches OCR — llm.Message has Native tagged json:"-").
//
// Text content is passed through Redact so credential-shaped strings never hit
// the JSONL. Applies to both plain-string Content and structured content
// blocks that carry Text (Claude's multi-part shape). ToolCall.Function.Arguments
// gets the same treatment because tool arguments are LLM-generated JSON that
// can legitimately contain diff snippets from user files.
func copyMessages(msgs []llm.Message) []llm.Message {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		out[i] = llm.Message{
			Role:             m.Role,
			Content:          redactContent(m.Content),
			ToolCallID:       m.ToolCallID,
			ToolCalls:        redactToolCalls(m.ToolCalls),
			Native:           m.Native,
			ReasoningContent: Redact(m.ReasoningContent),
		}
	}
	return out
}

// redactContent scrubs a Message.Content value. Content is `any` — either a
// plain string or a []ContentBlock (Claude multi-part). Unknown types are
// returned unchanged: they can't be marshaled meaningfully anyway.
func redactContent(c any) any {
	switch v := c.(type) {
	case string:
		return Redact(v)
	case []llm.ContentBlock:
		return redactBlocks(v)
	default:
		return c
	}
}

func redactBlocks(blocks []llm.ContentBlock) []llm.ContentBlock {
	if len(blocks) == 0 {
		return blocks
	}
	out := make([]llm.ContentBlock, len(blocks))
	for i, b := range blocks {
		out[i] = llm.ContentBlock{
			Type:      b.Type,
			Text:      Redact(b.Text),
			ToolUseID: b.ToolUseID,
			Content:   redactBlocks(b.Content),
		}
	}
	return out
}

func redactToolCalls(tcs []llm.ToolCall) []llm.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]llm.ToolCall, len(tcs))
	for i, tc := range tcs {
		out[i] = llm.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: llm.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: Redact(tc.Function.Arguments),
			},
		}
	}
	return out
}
