// SPDX-License-Identifier: Apache-2.0
// Portions Copyright 2026 disseqt
// Original abstraction distilled from alibaba/open-code-review internal/session.
//
// This file introduces a minimal Session interface used only by llmloop, so the
// package can be lifted into the target repository ahead of the real
// internal/session port. The interface is fulfilled by internal/session in a
// later phase — the shape here mirrors just what the loop calls.

package llmloop

import (
	"time"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
)

// TaskType names the kind of subtask a record belongs to. The string form is
// what llm.SessionTaskKey embeds when tagging a request for prompt-cache
// affinity, so the values must be stable across implementations.
type TaskType string

const (
	// MainTask — the primary tool-use loop conversation.
	MainTask TaskType = "main_task"
	// MemoryCompressionTask — the memory-compression LLM call issued when
	// the conversation crosses the soft/warning token thresholds.
	MemoryCompressionTask TaskType = "memory_compression_task"
)

// Session is the subset of the future internal/session.SessionHistory surface
// that llmloop needs. Callers pass a concrete impl; llmloop never constructs
// one. A nil Session panics at first use — same as the concrete type would.
//
// interface fulfilled by internal/session in a later phase
type Session interface {
	// SessionID returns the run-scoped session identifier used for cache
	// affinity keys (see llm.SessionTaskKey).
	SessionID() string
	// GetOrCreateFileSession returns the FileSession for taskKey, creating
	// one if it does not exist yet.
	GetOrCreateFileSession(taskKey string) FileSession
}

// FileSession is the per-subtask append-log surface. One FileSession backs
// every TaskRecord produced for that subtask, so records for the same task
// keep their ordering.
//
// interface fulfilled by internal/session in a later phase
type FileSession interface {
	AppendTaskRecord(taskType TaskType, msgs []llm.Message) TaskRecord
}

// TaskRecord is a single append-only record for one logical LLM request. The
// loop creates one before the request, then finalizes it with SetResponse or
// SetError once the request completes.
//
// interface fulfilled by internal/session in a later phase
type TaskRecord interface {
	SetError(err error, duration time.Duration)
	SetResponse(resp *llm.ChatResponse, duration time.Duration)
	AddToolFailure(name, args, errMsg string, duration time.Duration)
	AddToolResult(name, args, result string)
}
