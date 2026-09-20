// SPDX-License-Identifier: Apache-2.0
// Portions Copyright 2026 shubam-ai-code-reviewer contributors
// Adapted from alibaba/open-code-review internal/llmloop/loop.go

package llmloop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/comment"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/diff"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/tool"
)

// Deps bundles all per-call dependencies the Runner needs. Both diff review
// and full-file scan build a Deps from their own state and hand it to
// NewRunner.
type Deps struct {
	LLMClient         llm.LLMClient
	Model             string
	Template          Template
	Tools             *tool.Registry
	MainToolDefs      []llm.ToolDef
	CommentCollector  *comment.CommentCollector
	CommentWorkerPool *CommentWorkerPool
	Session           Session
	// DiffLookup is consulted by the code_comment tool path to resolve
	// line numbers against the file's diff (or against full file content
	// in scan mode — scan adapters return a synthetic Diff whose
	// NewFileContent is the whole file and Diff is empty).
	DiffLookup func(path string) *model.Diff

	// AllDiffs returns every diff this run reviews, for re-filing a comment
	// whose ExistingCode belongs to a different file than the one it was filed
	// against (diff.RelocateAcrossFiles). It is the reviewed set rather than
	// every parsed diff on purpose: re-filing a comment onto a path the run
	// excluded would point the reader at a file this review never covered.
	// When nil, cross-file re-filing is skipped and only same-file resolution
	// applies.
	AllDiffs func() []model.Diff
}

// Runner is a per-session (across subtasks) executor of the LLM tool-use
// loop. Token counters and warnings are aggregated across every RunMainTask
// call; background memory compression is scoped to each RunMainTask
// conversation (see compressionState).
type Runner struct {
	deps                  Deps
	totalInputTokens      int64 // atomically updated
	totalOutputTokens     int64
	totalCacheReadTokens  int64
	totalCacheWriteTokens int64
	warningsMu            sync.Mutex
	warnings              []AgentWarning
	toolCallsMu           sync.Mutex
	toolCalls             map[string]int64
	toolCallSequence      int64
	toolFailures          []ToolFailureDetail
	// bg tracks every background goroutine that can still issue an LLM
	// request after RunMainTask returned. WaitBackground joins them.
	bg sync.WaitGroup
}

// ToolFailureDetail describes one failed registered-tool invocation.
type ToolFailureDetail struct {
	ToolCallNumber int64  `json:"tool_call_number"`
	ToolName       string `json:"tool_name"`
	// FilePath carries the failing subtask's key, which is a file path only in
	// scan; a review group of several files reports its group key here.
	FilePath  string `json:"file_path,omitempty"`
	Arguments string `json:"arguments"`
	Error     string `json:"error"`
}

// NewRunner returns a Runner bound to the given dependencies.
func NewRunner(deps Deps) *Runner {
	return &Runner{deps: deps}
}

// WaitBackground blocks until every background job started by this Runner has
// returned. Background memory compression is the only such job, and
// cancelPendingCompression cancels it without waiting — its goroutine can
// therefore still be inside an LLM request after RunMainTask returned.
//
// Every pending job has already been cancelled by the time the last
// RunMainTask returns (cancelPendingCompression runs as a deferred call on
// every exit, and triggerAsyncCompression refuses to start a second job while
// one is pending), so this normally returns quickly — but the wait length
// ultimately depends on the LLM client honouring context cancellation.
func (r *Runner) WaitBackground() {
	r.bg.Wait()
}

// TotalInputTokens returns the accumulated input/prompt tokens from all LLM calls.
func (r *Runner) TotalInputTokens() int64 { return atomic.LoadInt64(&r.totalInputTokens) }

// TotalOutputTokens returns the accumulated completion tokens from all LLM calls.
func (r *Runner) TotalOutputTokens() int64 { return atomic.LoadInt64(&r.totalOutputTokens) }

// TotalCacheReadTokens returns the accumulated cache read tokens.
func (r *Runner) TotalCacheReadTokens() int64 { return atomic.LoadInt64(&r.totalCacheReadTokens) }

// TotalCacheWriteTokens returns the accumulated cache write tokens.
func (r *Runner) TotalCacheWriteTokens() int64 { return atomic.LoadInt64(&r.totalCacheWriteTokens) }

// TotalTokensUsed returns input + output.
func (r *Runner) TotalTokensUsed() int64 {
	return r.TotalInputTokens() + r.TotalOutputTokens()
}

// Warnings returns a copy of the accumulated warnings.
func (r *Runner) Warnings() []AgentWarning {
	r.warningsMu.Lock()
	defer r.warningsMu.Unlock()
	out := make([]AgentWarning, len(r.warnings))
	copy(out, r.warnings)
	return out
}

// RecordWarning adds a non-fatal warning.
func (r *Runner) RecordWarning(warningType, file, message string) {
	r.warningsMu.Lock()
	r.warnings = append(r.warnings, AgentWarning{
		File:    file,
		Message: message,
		Type:    warningType,
	})
	r.warningsMu.Unlock()
}

// ToolCalls returns a snapshot of the per-tool call counts.
func (r *Runner) ToolCalls() map[string]int64 {
	r.toolCallsMu.Lock()
	defer r.toolCallsMu.Unlock()
	out := make(map[string]int64, len(r.toolCalls))
	for k, v := range r.toolCalls {
		out[k] = v
	}
	return out
}

// ToolFailures returns failed tool calls ordered by their global call number.
func (r *Runner) ToolFailures() []ToolFailureDetail {
	r.toolCallsMu.Lock()
	defer r.toolCallsMu.Unlock()

	out := append([]ToolFailureDetail(nil), r.toolFailures...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ToolCallNumber < out[j].ToolCallNumber
	})
	return out
}

func (r *Runner) recordToolCall(name string) int64 {
	r.toolCallsMu.Lock()
	defer r.toolCallsMu.Unlock()
	if r.toolCalls == nil {
		r.toolCalls = make(map[string]int64)
	}
	r.toolCalls[name]++
	r.toolCallSequence++
	return r.toolCallSequence
}

func (r *Runner) recordToolFailure(number int64, name, taskKey, errMsg string,
	rec TaskRecord, rawArguments string, duration time.Duration) {
	detail := ToolFailureDetail{
		ToolCallNumber: number,
		ToolName:       name,
		FilePath:       taskKey,
		Arguments:      rawArguments,
		Error:          errMsg,
	}

	r.toolCallsMu.Lock()
	r.toolFailures = append(r.toolFailures, detail)
	r.toolCallsMu.Unlock()

	if rec != nil {
		rec.AddToolFailure(name, rawArguments, errMsg, duration)
	}
}

// RecordUsage adds the prompt/completion/cache tokens reported by an LLM
// response to the runner's aggregate counters. Used by callers that perform
// their own LLM calls outside RunMainTask.
func (r *Runner) RecordUsage(u *llm.UsageInfo) {
	if u == nil {
		return
	}
	atomic.AddInt64(&r.totalInputTokens, u.PromptTokens)
	atomic.AddInt64(&r.totalOutputTokens, u.CompletionTokens)
	atomic.AddInt64(&r.totalCacheReadTokens, u.CacheReadTokens)
	atomic.AddInt64(&r.totalCacheWriteTokens, u.CacheWriteTokens)
}

// CollectPendingComments awaits any async comment-processing workers and
// returns the aggregated comments from the collector. Safe to call once
// per session at the end.
func (r *Runner) CollectPendingComments() []model.LlmComment {
	if r.deps.CommentWorkerPool != nil {
		r.deps.CommentWorkerPool.Await()
	}
	return r.deps.CommentCollector.Comments()
}

// MainLoopStop classifies why RunMainTask stopped without an explicit task_done
// and without a Go error. It lets the caller attribute a precise, honest failure
// classification instead of guessing from free text: only a configured limit
// (max tool-request rounds) is a budget stop; the empty-round and compression
// exits are genuine but unclassifiable, so they map to the unknown catch-all.
// StopNone means the loop returned via task_done (completed) or via an error.
type MainLoopStop int

const (
	// StopNone — RunMainTask completed via task_done or returned an error; the
	// stop cause carries no additional meaning.
	StopNone MainLoopStop = iota
	// StopMaxRounds — the configured MaxToolRequestTimes round budget was
	// exhausted before task_done. This is a declared budget limit.
	StopMaxRounds
	// StopEmptyRounds — the model returned no usable tool result for too many
	// consecutive rounds. Not a declared budget; unclassifiable.
	StopEmptyRounds
	// StopCompression — context compression exceeded its threshold, so the loop
	// could not continue. Token/context driven but not a declared budget.
	StopCompression
)

// String names the stop for diagnostics — log lines and test failure messages.
// Without it a MainLoopStop formats as a bare integer, which tells the reader
// nothing about which exit fired.
func (s MainLoopStop) String() string {
	switch s {
	case StopNone:
		return "none"
	case StopMaxRounds:
		return "max_rounds"
	case StopEmptyRounds:
		return "empty_rounds"
	case StopCompression:
		return "compression"
	default:
		return fmt.Sprintf("MainLoopStop(%d)", int(s))
	}
}

// Reason is the safe, human-facing sentence for a stop, and the single source
// of truth for it. Each return is a static literal — never error text, a path
// or a provider payload — because callers persist it into machine-readable
// output. Every stop reads distinctly, including one added to the enum after
// this was written: the default names the unrecognized value instead of
// silently reusing the StopNone catch-all.
func (s MainLoopStop) Reason() string {
	switch s {
	case StopNone:
		return "main task stopped before completing"
	case StopMaxRounds:
		return "reached the maximum tool-request rounds without finishing"
	case StopEmptyRounds:
		return "stopped after repeated rounds without a usable tool result"
	case StopCompression:
		return "stopped because context compression exceeded its threshold"
	default:
		return fmt.Sprintf("main task stopped for an unrecognized reason (stop=%d)", int(s))
	}
}

// RunMainTask drives one MAIN_TASK conversation loop to its end. It sends
// messages with the configured tool definitions, executes any tool calls
// returned by the model, and collects review comments until task_done is
// called or limits are reached. Token usage and warnings are aggregated on the
// Runner across every call. The returned bool is true only when the model
// explicitly calls task_done with a successful state. The MainLoopStop return
// classifies a non-completed, non-error stop at its trigger point so the
// caller never has to infer the cause from text or context state.
//
// taskKey identifies the subtask this conversation belongs to, and is not
// necessarily a file path: scan passes one file's path, while review passes
// the group key of a file group that may hold several files. It is used for
// cache affinity, session bookkeeping, progress lines and as the fallback path
// for a code_comment the model filed without one.
func (r *Runner) RunMainTask(ctx context.Context, messages []llm.Message, taskKey string) (bool, MainLoopStop, error) {
	// Every round of this loop re-sends the growing conversation, so each
	// request is a prefix extension of the previous one — exactly what
	// provider prompt caches reuse. Scope the affinity key to this subtask's
	// main-task conversation so every round routes to the same cache node.
	ctx = llm.ContextWithSessionKey(ctx,
		llm.SessionTaskKey(r.deps.Session.SessionID(), string(MainTask), taskKey))

	toolReqCount := r.deps.Template.MaxToolRequestTimes
	const maxConsecutiveEmptyRounds = 3
	consecutiveEmptyRounds := 0
	sessionID := uuid.NewString()

	// Async compression is owned by this conversation alone; the deferred
	// cancel aborts any job still in flight when the conversation ends.
	st := &compressionState{}
	defer r.cancelPendingCompression(st)

	// stop defaults to StopMaxRounds: if the for-loop exits because
	// toolReqCount reached zero, the run stopped on the round budget. The
	// empty-round and compression breaks overwrite it at their trigger points.
	stop := StopMaxRounds
	for toolReqCount > 0 {
		select {
		case <-ctx.Done():
			return false, StopNone, ctx.Err()
		default:
		}

		toolReqCount--

		fs := r.deps.Session.GetOrCreateFileSession(taskKey)
		rec := fs.AppendTaskRecord(MainTask, append([]llm.Message(nil), messages...))
		startTime := time.Now()

		resp, err := r.deps.LLMClient.CompletionsWithCtx(ctx, llm.ChatRequest{
			Model:     r.deps.Model,
			Messages:  messages,
			Tools:     r.deps.MainToolDefs,
			MaxTokens: r.deps.Template.CompletionTokenLimit(),
			SessionID: sessionID,
		})
		duration := time.Since(startTime)
		if err != nil {
			rec.SetError(err, duration)
			return false, StopNone, fmt.Errorf("LLM completion error: %w", err)
		}
		rec.SetResponse(resp, duration)
		if resp.Usage != nil {
			atomic.AddInt64(&r.totalInputTokens, resp.Usage.PromptTokens)
			atomic.AddInt64(&r.totalOutputTokens, resp.Usage.CompletionTokens)
			atomic.AddInt64(&r.totalCacheReadTokens, resp.Usage.CacheReadTokens)
			atomic.AddInt64(&r.totalCacheWriteTokens, resp.Usage.CacheWriteTokens)
		}

		content := resp.VisibleContent()
		calls := resp.ToolCalls()

		if len(calls) == 0 {
			fmt.Fprintf(os.Stderr, "[sacr] No tool calls parsed for %s, retrying...\n", taskKey)
			messages = append(messages, llm.NewTextMessage("user", "You did not successfully call any tools. Please try again or use task_done if finished."))
			native := resp.Native()
			reasoning := resp.ReasoningContent()
			if content != "" || native.Payload != nil || reasoning != "" {
				messages = append(messages[:len(messages)-1], llm.NewToolCallMessage(content, nil, native, reasoning), messages[len(messages)-1])
			}
			continue
		}

		var results []tool.ToolCallResult
		taskCompleted := false
		hasValidResult := false

		// Capture the model's native reasoning content for this turn. Models
		// without a reasoning channel leave it empty. Reasoning is turn-level,
		// so all tool calls in this turn share it.
		thinking := resp.ReasoningContent()
		for _, call := range calls {
			cp := r.executeToolCall(ctx, taskKey, call, rec, thinking)
			if cp.Failed {
				return false, StopNone, fmt.Errorf("task failed: %s", cp.Data)
			} else if cp.Completed {
				results = append(results, tool.ToolCallResult{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Result:     "Task completed successfully.",
				})
				taskCompleted = true
			} else if cp.Data != "" {
				results = append(results, tool.ToolCallResult{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Result:     cp.Data,
				})
				hasValidResult = true
			} else {
				results = append(results, tool.ToolCallResult{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Result:     "Error: Tool execution returned no result.",
				})
			}
		}

		if taskCompleted {
			return true, StopNone, nil
		}
		if !hasValidResult {
			consecutiveEmptyRounds++
			if consecutiveEmptyRounds >= maxConsecutiveEmptyRounds {
				fmt.Fprintf(os.Stderr, "[sacr] Too many empty retries for %s, stopping.\n", taskKey)
				stop = StopEmptyRounds
				break
			}
			fmt.Fprintf(os.Stderr, "[sacr] No valid tool results for %s, retrying...\n", taskKey)
		} else {
			consecutiveEmptyRounds = 0
		}

		succeed := r.addNextMessage(ctx, content, calls, resp.Native(), thinking, results, &messages, taskKey, st)
		if !succeed {
			fmt.Fprintf(os.Stderr, "[sacr] Context compression exceeded threshold for %s, stopping.\n", taskKey)
			stop = StopCompression
			break
		}
	}

	if stop == StopMaxRounds {
		fmt.Fprintf(os.Stderr, "[sacr] Max tool requests reached for %s.\n", taskKey)
		r.runGraceRound(ctx, messages, taskKey, sessionID)
	}
	return false, stop, nil
}

// runGraceRound performs one final LLM call after the tool-request budget is
// exhausted, giving the model a chance to submit any findings it identified
// but did not yet report via code_comment.
func (r *Runner) runGraceRound(ctx context.Context, messages []llm.Message, taskKey string, sessionID string) {
	graceDefs := graceRoundToolDefs(r.deps.MainToolDefs)
	if len(graceDefs) == 0 {
		return
	}

	messages = append(messages, llm.NewTextMessage("user",
		"Your tool-call budget is exhausted. This is your FINAL round. You may ONLY:\n"+
			"- Call code_comment to submit any findings you have identified but not yet reported.\n"+
			"- Call task_done if you have nothing more to report.\n"+
			"No other tools are available. Do not attempt further analysis."))

	if ctx.Err() != nil {
		fmt.Fprintf(os.Stderr, "[sacr] Grace round skipped for %s: context cancelled\n", taskKey)
		return
	}

	fs := r.deps.Session.GetOrCreateFileSession(taskKey)
	rec := fs.AppendTaskRecord(MainTask, messages)
	startTime := time.Now()

	resp, err := r.deps.LLMClient.CompletionsWithCtx(ctx, llm.ChatRequest{
		Model:     r.deps.Model,
		Messages:  messages,
		Tools:     graceDefs,
		MaxTokens: r.deps.Template.CompletionTokenLimit(),
		SessionID: sessionID,
	})
	duration := time.Since(startTime)
	if err != nil {
		rec.SetError(err, duration)
		fmt.Fprintf(os.Stderr, "[sacr] Grace round LLM error for %s: %v\n", taskKey, err)
		return
	}

	rec.SetResponse(resp, duration)
	if resp.Usage != nil {
		atomic.AddInt64(&r.totalInputTokens, resp.Usage.PromptTokens)
		atomic.AddInt64(&r.totalOutputTokens, resp.Usage.CompletionTokens)
		atomic.AddInt64(&r.totalCacheReadTokens, resp.Usage.CacheReadTokens)
		atomic.AddInt64(&r.totalCacheWriteTokens, resp.Usage.CacheWriteTokens)
	}

	calls := resp.ToolCalls()
	if len(calls) == 0 {
		return
	}

	thinking := resp.ReasoningContent()
	for _, call := range calls {
		r.executeToolCall(ctx, taskKey, call, rec, thinking)
	}
}

// graceRoundToolDefs returns the subset of tool definitions containing only
// code_comment and task_done.
func graceRoundToolDefs(defs []llm.ToolDef) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, 2)
	for _, d := range defs {
		if d.Function.Name == "code_comment" || d.Function.Name == "task_done" {
			out = append(out, d)
		}
	}
	return out
}

// executeToolCall dispatches a single tool call from the LLM response and
// records the result in session history. code_comment handling includes
// optional async dispatch through CommentWorkerPool plus line-number
// resolution.
//
// taskKey is the calling subtask's key (see RunMainTask). It labels the
// session records and the worker-pool submission, and stands in as a
// code_comment's path when the model omitted one.
func (r *Runner) executeToolCall(ctx context.Context, taskKey string, call llm.ToolCall, rec TaskRecord, thinking string) tool.TaskCheckpoint {
	t := tool.OfName(call.Function.Name)

	if t == tool.TaskDone {
		args, err := parseToolArgs(call.Function.Arguments)
		if err != nil {
			return tool.Of(fmt.Sprintf("Error parsing tool arguments for %s: %v", t.Name(), err))
		}
		rawState, hasState := args["state"]
		if !hasState {
			return tool.Complete()
		}
		state, ok := rawState.(string)
		if !ok {
			return tool.Of("Error: task_done state must be DONE or FAILED.")
		}
		switch state {
		case "DONE":
			return tool.Complete()
		case "FAILED":
			return tool.Fail("task_done reported FAILED")
		default:
			return tool.Of(fmt.Sprintf("Error: invalid task_done state %q; expected DONE or FAILED.", state))
		}
	}

	toolName := call.Function.Name
	p, found := r.deps.Tools.Get(toolName)
	if !found {
		return tool.Of(tool.NotAvailableMsg)
	}

	toolCallNumber := r.recordToolCall(toolName)
	callStarted := time.Now()

	args, err := parseToolArgs(call.Function.Arguments)
	if err != nil {
		errMsg := fmt.Sprintf("Error parsing tool arguments for %s: %v", toolName, err)
		r.recordToolFailure(toolCallNumber, toolName, taskKey, errMsg,
			rec, call.Function.Arguments, time.Since(callStarted))
		return tool.Of(errMsg)
	}

	startTime := time.Now()

	if t == tool.CodeComment {
		comments, repair, errMsg := comment.ParseCommentsWithPath(args, taskKey)
		if repair != nil {
			// The model sees a plain success, so this warning is the only
			// record that its `comments` violated the array schema — without
			// it the repair would absorb an unbounded number of them
			// unobserved.
			r.RecordWarning("comment_args_repaired", taskKey, repair.Message())
		}
		if errMsg != "" {
			r.recordToolFailure(toolCallNumber, toolName, taskKey, errMsg,
				rec, call.Function.Arguments, time.Since(startTime))
			return tool.Of(errMsg)
		}

		// Batched comments share the turn's thinking.
		if thinking != "" {
			for i := range comments {
				if comments[i].Thinking == "" {
					comments[i].Thinking = thinking
				}
			}
		}

		resolveAndCollect := func(rctx context.Context) {
			sysPrompt, userTmpl := r.deps.Template.ReLocationPrompts()
			for i := range comments {
				cm := &comments[i]
				var d *model.Diff
				if r.deps.DiffLookup != nil {
					d = r.deps.DiffLookup(cm.Path)
				}
				// Resolution order: the comment's own file, then a cross-file
				// search, then an LLM re-write of ExistingCode. The cross-file
				// search runs even when d is nil, since a comment filed against
				// a path this run holds no diff for is exactly the case that
				// search can still place.
				if d != nil && diff.ResolveComment(cm, d) {
					r.deps.CommentCollector.Add(*cm)
					continue
				}
				if r.deps.AllDiffs != nil {
					from := cm.Path
					if to, ok := diff.RelocateAcrossFiles(cm, r.deps.AllDiffs()); ok {
						r.RecordWarning("comment_refiled", to, fmt.Sprintf(
							"comment filed against %s describes code in %s; re-filed", from, to))
						r.deps.CommentCollector.Add(*cm)
						continue
					}
				}
				// Both deterministic paths failed. Ask the LLM to rewrite
				// ExistingCode from the diff, then retry ResolveComment.
				if d != nil && r.deps.LLMClient != nil && userTmpl != "" {
					msgs := diff.BuildReLocationMessages(cm, d, sysPrompt, userTmpl)
					if ok, _ := diff.ReLocateComment(rctx, cm, d, r.deps.LLMClient, msgs, r.deps.Model, r.deps.Template.CompletionTokenLimit()); ok {
						r.RecordWarning("comment_relocated_by_llm", cm.Path,
							"LLM re-generated existing_code snippet to snap the comment onto the diff")
					}
				}
				r.deps.CommentCollector.Add(*cm)
			}
		}

		if r.deps.CommentWorkerPool != nil {
			if rec != nil {
				rec.AddToolResult(t.Name(), call.Function.Arguments, "(async)")
			}
			pool := r.deps.CommentWorkerPool
			asyncCtx := context.WithoutCancel(ctx)
			pool.SubmitFor(taskKey, func() ([]model.LlmComment, error) {
				resolveAndCollect(asyncCtx)
				return []model.LlmComment{}, nil
			})
			return tool.Of(tool.CommentSucceed)
		}

		resolveAndCollect(ctx)
		if rec != nil {
			rec.AddToolResult(t.Name(), call.Function.Arguments, tool.CommentSucceed)
		}
		return tool.Of(tool.CommentSucceed)
	}

	// Synchronous path for all other tools
	result, err := p.Execute(ctx, args)
	dur := time.Since(startTime)

	if err != nil {
		r.recordToolFailure(toolCallNumber, toolName, taskKey, err.Error(),
			rec, call.Function.Arguments, dur)
		return tool.Of(fmt.Sprintf("Error executing tool %s: %v", toolName, err))
	}
	if rec != nil {
		rec.AddToolResult(toolName, call.Function.Arguments, result)
	}
	return tool.Of(result)
}

// addNextMessage extends the conversation with the assistant message and
// tool responses, applying three-zone compression at the soft (60%) and
// warning (80%) MaxTokens thresholds. Returns false when even after
// synchronous compression the conversation is still over the warning
// threshold — caller should stop the loop in that case.
func (r *Runner) addNextMessage(ctx context.Context, assistantContent string, toolCalls []llm.ToolCall, native llm.NativeTurn, reasoningContent string, results []tool.ToolCallResult, messages *[]llm.Message, taskKey string, st *compressionState) bool {
	maxAllowed := r.deps.Template.MaxTokens
	softLimit := int(float64(maxAllowed) * tokenSoftThreshold)
	warnLimit := PromptTokenLimit(maxAllowed)

	r.tryApplyPendingCompression(st, messages)

	// A conversation can already be over the warning threshold before this
	// round's messages are appended (e.g. an oversized initial prompt).
	if CountMessagesTokens(*messages) > warnLimit {
		r.cancelPendingCompression(st)
		var err error
		if *messages, err = r.runCompression(ctx, *messages, taskKey); err != nil {
			// Compression failed; continue with over-limit messages — the
			// post-append check below will retry.
			fmt.Fprintf(os.Stderr, "[sacr] Memory compression failed: %v\n", err)
		}
	}

	if len(toolCalls) > 0 {
		*messages = append(*messages, llm.NewToolCallMessage(assistantContent, toolCalls, native, reasoningContent))
	} else if assistantContent != "" || native.Payload != nil {
		*messages = append(*messages, llm.NewToolCallMessage(assistantContent, nil, native, reasoningContent))
	}

	for _, rs := range results {
		*messages = append(*messages, llm.NewToolResultMessage(rs.ToolCallID, rs.Result))
	}

	finalCount := CountMessagesTokens(*messages)
	if finalCount > warnLimit {
		r.cancelPendingCompression(st)
		var err error
		if *messages, err = r.runCompression(ctx, *messages, taskKey); err != nil {
			fmt.Fprintf(os.Stderr, "[sacr] Memory compression failed: %v\n", err)
		}
		finalCount = CountMessagesTokens(*messages)
	}

	// Trigger async compression only after all appends for this update, so
	// a job is never started and then immediately cancelled by the same call,
	// and never started when we are about to return false.
	if finalCount > softLimit && finalCount < warnLimit {
		r.triggerAsyncCompression(ctx, st, *messages, taskKey)
	}

	return finalCount < warnLimit
}

// parseToolArgs unmarshals a tool call's raw JSON arguments, always
// returning a non-nil map on success: some OpenAI-compatible gateways send
// "arguments": null, which unmarshals to a nil map and would panic on the
// first write.
func parseToolArgs(raw string) (map[string]any, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = make(map[string]any)
	}
	return args, nil
}
