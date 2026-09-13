// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt
//
// Adapted from alibaba/open-code-review internal/session/history.go under
// Apache License 2.0. Modifications: dropped viewer, manifest coverage, and
// sealed-input identity; simplified to the interface declared by
// internal/llmloop.TaskRecord.

package session

import (
	"sync/atomic"
	"time"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llmloop"
)

// TaskRecord is the per-request handle returned by
// FileSession.AppendTaskRecord. Its methods append llm_response, llm_error,
// or tool_call records for one LLM turn. Safe to call from one goroutine at
// a time (llmloop drives one request end-to-end sequentially).
type TaskRecord struct {
	fileSession *FileSession
	taskType    llmloop.TaskType
	requestNo   int
}

// SetResponse appends an llm_response record. A nil resp (or one with no
// choices) is degraded to an empty-content record rather than a panic — the
// caller has already lost the response, no need to make it worse.
func (r *TaskRecord) SetResponse(resp *llm.ChatResponse, dur time.Duration) {
	var (
		content, reasoning, model string
		toolCalls                 []llm.ToolCall
		usage                     *llm.UsageInfo
	)
	if resp != nil {
		model = resp.Model
		usage = resp.Usage
		if len(resp.Choices) > 0 {
			msg := resp.Choices[0].Message
			if msg.Content != nil {
				content = *msg.Content
			}
			reasoning = msg.ReasoningContent
			toolCalls = msg.ToolCalls
		}
	}

	s := r.fileSession.session
	s.mu.Lock()
	defer s.mu.Unlock()

	base := s.newBaseLocked(typeLLMResponse)
	rec := llmResponseRecord{
		baseRecord:       base,
		FilePath:         r.fileSession.taskKey,
		TaskType:         string(r.taskType),
		Content:          Redact(content),
		ReasoningContent: Redact(reasoning),
		ToolCalls:        redactToolCalls(toolCalls),
		Model:            model,
		DurationMs:       dur.Milliseconds(),
		Usage:            usage,
	}
	s.commitLocked(base.UUID, rec)
}

// SetError appends an llm_error record and bumps the run-wide failure count.
func (r *TaskRecord) SetError(err error, dur time.Duration) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}

	s := r.fileSession.session
	atomic.AddInt64(&s.llmFailures, 1)

	s.mu.Lock()
	defer s.mu.Unlock()

	base := s.newBaseLocked(typeLLMError)
	rec := llmErrorRecord{
		baseRecord: base,
		FilePath:   r.fileSession.taskKey,
		TaskType:   string(r.taskType),
		RequestNo:  r.requestNo,
		Error:      msg,
		DurationMs: dur.Milliseconds(),
	}
	s.commitLocked(base.UUID, rec)
}

// AddToolResult appends a successful tool_call record (ok=true, duration=0).
func (r *TaskRecord) AddToolResult(name, args, result string) {
	r.writeToolCall(name, args, result, "", true, 0)
}

// AddToolFailure appends a failed tool_call record (ok=false). The errMsg is
// placed in the result field so viewers show it alongside successful output.
func (r *TaskRecord) AddToolFailure(name, args, errMsg string, dur time.Duration) {
	r.writeToolCall(name, args, "", errMsg, false, dur)
}

func (r *TaskRecord) writeToolCall(name, args, result, errMsg string, ok bool, dur time.Duration) {
	// Failed calls put the error message in Result so a single field carries
	// both outcomes — matches OCR's shape and avoids a second column that
	// would be empty on success.
	body := result
	if !ok {
		body = errMsg
	}

	s := r.fileSession.session
	s.mu.Lock()
	defer s.mu.Unlock()

	base := s.newBaseLocked(typeToolCall)
	rec := toolCallRecord{
		baseRecord: base,
		FilePath:   r.fileSession.taskKey,
		TaskType:   string(r.taskType),
		ToolName:   name,
		Arguments:  Redact(args),
		Result:     Redact(body),
		Ok:         ok,
		DurationMs: dur.Milliseconds(),
	}
	s.commitLocked(base.UUID, rec)
}
