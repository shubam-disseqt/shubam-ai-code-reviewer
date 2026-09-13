// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package session

import (
	"time"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
)

// Record type discriminators — the "type" field of every JSONL line.
const (
	typeSessionStart = "session_start"
	typeLLMRequest   = "llm_request"
	typeLLMResponse  = "llm_response"
	typeLLMError     = "llm_error"
	typeToolCall     = "tool_call"
	typeSessionEnd   = "session_end"
)

// baseRecord is the header every JSONL line shares. parentUuid chains records
// within one file session; the first record has parentUuid == sessionStart's uuid.
type baseRecord struct {
	UUID       string    `json:"uuid"`
	ParentUUID string    `json:"parentUuid"`
	Type       string    `json:"type"`
	SessionID  string    `json:"sessionId"`
	Timestamp  time.Time `json:"timestamp"`
}

// sessionStartRecord is written once at Session construction.
type sessionStartRecord struct {
	baseRecord
	CWD        string `json:"cwd,omitempty"`
	GitBranch  string `json:"gitBranch,omitempty"`
	Model      string `json:"model,omitempty"`
	ReviewMode string `json:"reviewMode,omitempty"`
	DiffFrom   string `json:"diffFrom,omitempty"`
	DiffTo     string `json:"diffTo,omitempty"`
	DiffCommit string `json:"diffCommit,omitempty"`
}

// llmRequestRecord is written by FileSession.AppendTaskRecord.
type llmRequestRecord struct {
	baseRecord
	FilePath  string        `json:"filePath"`
	TaskType  string        `json:"taskType"`
	RequestNo int           `json:"request_no"`
	Messages  []llm.Message `json:"messages"`
}

// llmResponseRecord is written by TaskRecord.SetResponse.
type llmResponseRecord struct {
	baseRecord
	FilePath         string         `json:"filePath"`
	TaskType         string         `json:"taskType"`
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []llm.ToolCall `json:"tool_calls,omitempty"`
	Model            string         `json:"model,omitempty"`
	DurationMs       int64          `json:"duration_ms"`
	Usage            *llm.UsageInfo `json:"usage,omitempty"`
}

// llmErrorRecord is written by TaskRecord.SetError.
type llmErrorRecord struct {
	baseRecord
	FilePath   string `json:"filePath"`
	TaskType   string `json:"taskType"`
	RequestNo  int    `json:"request_no"`
	Error      string `json:"error"`
	DurationMs int64  `json:"duration_ms"`
}

// toolCallRecord is written by TaskRecord.AddToolResult / AddToolFailure.
type toolCallRecord struct {
	baseRecord
	FilePath   string `json:"filePath"`
	TaskType   string `json:"taskType"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments"`
	Result     string `json:"result"`
	Ok         bool   `json:"ok"`
	DurationMs int64  `json:"duration_ms"`
}

// sessionEndRecord is written by Session.Close.
type sessionEndRecord struct {
	baseRecord
	FilesReviewed   int     `json:"files_reviewed"`
	DurationSeconds float64 `json:"duration_seconds"`
	LLMFailures     int     `json:"llm_failures"`
}
