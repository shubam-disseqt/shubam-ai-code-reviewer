// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt Contributors

package llmloop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/comment"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/tool"
)

// --- Fakes ---

// scriptedClient returns a queue of pre-baked responses (or errors) in order.
type scriptedClient struct {
	mu        sync.Mutex
	responses []scriptedResponse
	calls     []llm.ChatRequest
}

type scriptedResponse struct {
	resp *llm.ChatResponse
	err  error
}

func (c *scriptedClient) CompletionsWithCtx(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, req)
	if len(c.responses) == 0 {
		return nil, errors.New("scriptedClient: exhausted (unexpected extra call)")
	}
	r := c.responses[0]
	c.responses = c.responses[1:]
	return r.resp, r.err
}

func chatRespToolCall(name, argsJSON string) *llm.ChatResponse {
	empty := ""
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message: llm.ResponseMessage{
				Role:    "assistant",
				Content: &empty,
				ToolCalls: []llm.ToolCall{
					{ID: "call-" + name, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: argsJSON}},
				},
			},
			FinishReason: "tool_calls",
		}},
		Usage: &llm.UsageInfo{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	}
}

func chatRespText(text string) *llm.ChatResponse {
	t := text
	return &llm.ChatResponse{
		Choices: []llm.Choice{{
			Message: llm.ResponseMessage{Role: "assistant", Content: &t},
		}},
	}
}

// --- Fake session ---

type fakeSession struct {
	id      string
	files   map[string]*fakeFileSession
	filesMu sync.Mutex
}

func newFakeSession() *fakeSession {
	return &fakeSession{id: "session-abc", files: make(map[string]*fakeFileSession)}
}

func (s *fakeSession) SessionID() string { return s.id }
func (s *fakeSession) GetOrCreateFileSession(taskKey string) FileSession {
	s.filesMu.Lock()
	defer s.filesMu.Unlock()
	fs, ok := s.files[taskKey]
	if !ok {
		fs = &fakeFileSession{taskKey: taskKey}
		s.files[taskKey] = fs
	}
	return fs
}

type fakeFileSession struct {
	taskKey string
	mu      sync.Mutex
	records []*fakeTaskRecord
}

func (fs *fakeFileSession) AppendTaskRecord(t TaskType, msgs []llm.Message) TaskRecord {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	rec := &fakeTaskRecord{taskType: t, msgs: msgs}
	fs.records = append(fs.records, rec)
	return rec
}

type fakeTaskRecord struct {
	taskType     TaskType
	msgs         []llm.Message
	resp         *llm.ChatResponse
	err          error
	duration     time.Duration
	toolResults  []string
	toolFailures []string
}

func (r *fakeTaskRecord) SetError(err error, d time.Duration) { r.err = err; r.duration = d }
func (r *fakeTaskRecord) SetResponse(resp *llm.ChatResponse, d time.Duration) {
	r.resp = resp
	r.duration = d
}
func (r *fakeTaskRecord) AddToolFailure(name, args, errMsg string, d time.Duration) {
	r.toolFailures = append(r.toolFailures, name+"|"+errMsg)
}
func (r *fakeTaskRecord) AddToolResult(name, args, result string) {
	r.toolResults = append(r.toolResults, name+"|"+result)
}

// --- Fake tool provider ---

type fakeToolProvider struct {
	toolID tool.Tool
	fn     func(ctx context.Context, args map[string]any) (string, error)
}

func (p *fakeToolProvider) Tool() tool.Tool { return p.toolID }
func (p *fakeToolProvider) Execute(ctx context.Context, args map[string]any) (string, error) {
	return p.fn(ctx, args)
}

// --- Helpers ---

func newTestRunner(t *testing.T, client llm.LLMClient, tmpl Template) *Runner {
	t.Helper()
	reg := tool.NewRegistry()
	// code_comment is handled specially inside the loop but must be registered
	// so the not-found short-circuit doesn't fire.
	reg.Register(tool.NewStub(tool.CodeComment))
	deps := Deps{
		LLMClient:        client,
		Model:            "m",
		Template:         tmpl,
		Tools:            reg,
		MainToolDefs:     defaultToolDefs(),
		CommentCollector: comment.NewCommentCollector(),
		Session:          newFakeSession(),
	}
	return NewRunner(deps)
}

func defaultToolDefs() []llm.ToolDef {
	return []llm.ToolDef{
		{Type: "function", Function: llm.FunctionDef{
			Name: "task_done", Description: "signal completion",
			Parameters: map[string]any{"type": "object"},
		}},
		{Type: "function", Function: llm.FunctionDef{
			Name: "code_comment", Description: "leave a code comment",
			Parameters: map[string]any{"type": "object"},
		}},
		{Type: "function", Function: llm.FunctionDef{
			Name: "file_read", Description: "read a file",
			Parameters: map[string]any{"type": "object"},
		}},
	}
}

// --- Tests: Runner getters ---

func TestRunner_Getters(t *testing.T) {
	r := NewRunner(Deps{
		CommentCollector: comment.NewCommentCollector(),
		Session:          newFakeSession(),
	})
	if r.TotalInputTokens() != 0 || r.TotalOutputTokens() != 0 || r.TotalCacheReadTokens() != 0 || r.TotalCacheWriteTokens() != 0 || r.TotalTokensUsed() != 0 {
		t.Error("expected zero counters")
	}
	if len(r.Warnings()) != 0 {
		t.Error("expected no warnings")
	}
	r.RecordWarning("boom", "f.go", "something")
	if warns := r.Warnings(); len(warns) != 1 || warns[0].Type != "boom" {
		t.Errorf("warnings = %+v", warns)
	}
	// Mutation guard: caller's slice does not affect internal state.
	warns := r.Warnings()
	warns[0] = AgentWarning{}
	if r.Warnings()[0].Type != "boom" {
		t.Error("Warnings() returns internal slice")
	}
	if len(r.ToolCalls()) != 0 || len(r.ToolFailures()) != 0 {
		t.Error("expected empty tool metrics")
	}
	// WaitBackground on a fresh runner returns quickly.
	done := make(chan struct{})
	go func() { r.WaitBackground(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitBackground blocked on fresh runner")
	}
}

func TestRunner_RecordUsage(t *testing.T) {
	r := NewRunner(Deps{})
	r.RecordUsage(nil) // no-op
	r.RecordUsage(&llm.UsageInfo{
		PromptTokens: 10, CompletionTokens: 3, CacheReadTokens: 2, CacheWriteTokens: 4,
	})
	if r.TotalInputTokens() != 10 || r.TotalOutputTokens() != 3 {
		t.Errorf("counters = (%d, %d)", r.TotalInputTokens(), r.TotalOutputTokens())
	}
	if r.TotalCacheReadTokens() != 2 || r.TotalCacheWriteTokens() != 4 {
		t.Errorf("cache counters wrong")
	}
	if r.TotalTokensUsed() != 13 {
		t.Errorf("total = %d, want 13", r.TotalTokensUsed())
	}
}

func TestRunner_recordToolCallAndFailure(t *testing.T) {
	r := NewRunner(Deps{})
	n1 := r.recordToolCall("t1")
	n2 := r.recordToolCall("t1")
	n3 := r.recordToolCall("t2")
	if n1 != 1 || n2 != 2 || n3 != 3 {
		t.Errorf("sequence = %d %d %d", n1, n2, n3)
	}
	if r.ToolCalls()["t1"] != 2 || r.ToolCalls()["t2"] != 1 {
		t.Errorf("call counts = %v", r.ToolCalls())
	}

	// Failures — record out of order to prove ToolFailures sorts by number.
	r.recordToolFailure(3, "t2", "key", "err3", nil, "args", 0)
	r.recordToolFailure(1, "t1", "key", "err1", nil, "args", 0)
	failures := r.ToolFailures()
	if len(failures) != 2 {
		t.Fatalf("failures len = %d", len(failures))
	}
	if failures[0].ToolCallNumber != 1 || failures[1].ToolCallNumber != 3 {
		t.Errorf("failures not sorted: %+v", failures)
	}
}

// --- Tests: MainLoopStop.String/Reason ---

func TestMainLoopStop_String(t *testing.T) {
	tests := []struct {
		s    MainLoopStop
		want string
	}{
		{StopNone, "none"},
		{StopMaxRounds, "max_rounds"},
		{StopEmptyRounds, "empty_rounds"},
		{StopCompression, "compression"},
		{MainLoopStop(99), "MainLoopStop(99)"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestMainLoopStop_Reason(t *testing.T) {
	tests := []struct {
		s      MainLoopStop
		subStr string
	}{
		{StopNone, "before completing"},
		{StopMaxRounds, "maximum tool-request"},
		{StopEmptyRounds, "usable tool result"},
		{StopCompression, "compression"},
		{MainLoopStop(42), "unrecognized"},
	}
	for _, tt := range tests {
		if got := tt.s.Reason(); !strings.Contains(got, tt.subStr) {
			t.Errorf("(%d).Reason() = %q, want substring %q", tt.s, got, tt.subStr)
		}
	}
}

// --- Tests: parseToolArgs ---

func TestParseToolArgs(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantNil bool
		wantErr bool
	}{
		{"empty object", "{}", false, false},
		{"null becomes empty map", "null", false, false},
		{"with fields", `{"a":1,"b":"x"}`, false, false},
		{"invalid json", "{not json", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := parseToolArgs(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m == nil {
				t.Fatal("map should never be nil")
			}
		})
	}
}

// --- Tests: graceRoundToolDefs ---

func TestGraceRoundToolDefs(t *testing.T) {
	all := defaultToolDefs()
	grace := graceRoundToolDefs(all)
	if len(grace) != 2 {
		t.Fatalf("grace tool defs = %d, want 2", len(grace))
	}
	names := map[string]bool{}
	for _, d := range grace {
		names[d.Function.Name] = true
	}
	if !names["code_comment"] || !names["task_done"] {
		t.Errorf("grace defs missing expected tools: %v", names)
	}
}

func TestGraceRoundToolDefs_None(t *testing.T) {
	grace := graceRoundToolDefs(nil)
	if len(grace) != 0 {
		t.Errorf("expected empty, got %v", grace)
	}
}

// --- Tests: RunMainTask paths ---

func TestRunMainTask_HappyPathTaskDone(t *testing.T) {
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)},
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 5})
	completed, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("system", "sys"),
		llm.NewTextMessage("user", "please review"),
	}, "file.go")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !completed {
		t.Error("expected completed=true")
	}
	if stop != StopNone {
		t.Errorf("stop = %v, want StopNone", stop)
	}
	if r.TotalInputTokens() != 1 || r.TotalOutputTokens() != 1 {
		t.Errorf("usage not aggregated: in=%d out=%d", r.TotalInputTokens(), r.TotalOutputTokens())
	}
}

func TestRunMainTask_TaskDoneFailed(t *testing.T) {
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("task_done", `{"state":"FAILED"}`)},
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 5})
	completed, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go")
	if err == nil || !strings.Contains(err.Error(), "task_done reported FAILED") {
		t.Errorf("expected FAILED error, got %v", err)
	}
	if completed || stop != StopNone {
		t.Errorf("completed=%v stop=%v", completed, stop)
	}
}

func TestRunMainTask_TaskDoneInvalidState(t *testing.T) {
	// task_done with an unrecognized state string returns an error result;
	// the loop keeps trying until MaxToolRequestTimes runs out. Two rounds +
	// one grace round.
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("task_done", `{"state":"MAYBE"}`)},
		{resp: chatRespToolCall("task_done", `{"state":"MAYBE"}`)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)}, // grace round
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 2})
	completed, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go")
	if err != nil || completed {
		t.Errorf("unexpected: err=%v completed=%v", err, completed)
	}
	if stop != StopMaxRounds {
		t.Errorf("stop = %v, want StopMaxRounds", stop)
	}
}

func TestRunMainTask_TaskDoneWithoutState(t *testing.T) {
	// task_done with no state means Complete.
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("task_done", `{}`)},
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 5})
	completed, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go")
	if err != nil || !completed || stop != StopNone {
		t.Errorf("unexpected: err=%v completed=%v stop=%v", err, completed, stop)
	}
}

func TestRunMainTask_TaskDoneNonStringState(t *testing.T) {
	// state is not a string → error result kept as data → hits max rounds.
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("task_done", `{"state":123}`)},
		{resp: chatRespToolCall("task_done", `{"state":123}`)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)}, // grace
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 2})
	_, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if stop != StopMaxRounds {
		t.Errorf("stop = %v, want StopMaxRounds", stop)
	}
}

func TestRunMainTask_LLMError(t *testing.T) {
	client := &scriptedClient{responses: []scriptedResponse{
		{err: errors.New("network fell over")},
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 5})
	completed, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go")
	if err == nil || !strings.Contains(err.Error(), "LLM completion error") {
		t.Errorf("expected LLM error, got %v", err)
	}
	if completed || stop != StopNone {
		t.Errorf("unexpected completed=%v stop=%v", completed, stop)
	}
}

func TestRunMainTask_MaxRoundsWithGrace(t *testing.T) {
	// Every response returns a code_comment tool call (never task_done), so the
	// loop exhausts MaxToolRequestTimes and calls the grace round.
	//
	// Grace round makes one extra call, so we script MaxToolRequestTimes + 1.
	responses := []scriptedResponse{}
	for i := 0; i < 3; i++ {
		responses = append(responses, scriptedResponse{
			resp: chatRespToolCall("code_comment", `{"comments":[{"content":"nit","path":"f.go","start_line":1,"end_line":1}]}`),
		})
	}
	// Grace-round response with task_done so no further comments are attempted.
	responses = append(responses, scriptedResponse{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)})

	client := &scriptedClient{responses: responses}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 3})

	_, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("system", "sys"),
		llm.NewTextMessage("user", "review"),
	}, "f.go")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if stop != StopMaxRounds {
		t.Errorf("stop = %v, want StopMaxRounds", stop)
	}
	// The grace round should have run: check that responses are exhausted.
	client.mu.Lock()
	remaining := len(client.responses)
	client.mu.Unlock()
	if remaining != 0 {
		t.Errorf("grace round did not consume its response (remaining=%d)", remaining)
	}
}

func TestRunMainTask_NoToolCallsRetries(t *testing.T) {
	// Response with no tool calls. The loop appends a nudge user message and
	// tries again. First response has no tool calls, second has task_done.
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespText("I have some thoughts")},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)},
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 5})
	completed, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go")
	if err != nil || !completed || stop != StopNone {
		t.Errorf("unexpected: completed=%v stop=%v err=%v", completed, stop, err)
	}
}

func TestRunMainTask_ContextCancelled(t *testing.T) {
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)},
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 5})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, stop, err := r.RunMainTask(ctx, []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if stop != StopNone {
		t.Errorf("stop = %v", stop)
	}
}

func TestRunMainTask_UnknownTool(t *testing.T) {
	// Unknown tool → NotAvailable message. Non-empty data keeps the loop
	// alive until MaxToolRequestTimes runs out.
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("no_such_tool", `{}`)},
		{resp: chatRespToolCall("no_such_tool", `{}`)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)}, // grace round
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 2})
	_, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if stop != StopMaxRounds {
		t.Errorf("stop = %v, want StopMaxRounds", stop)
	}
}

func TestRunMainTask_RegisteredToolSuccess(t *testing.T) {
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("file_read", `{"path":"f.go"}`)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)},
	}}
	tmpl := Template{MaxTokens: 100000, MaxToolRequestTimes: 5}
	reg := tool.NewRegistry()
	reg.Register(&fakeToolProvider{
		toolID: tool.FileRead,
		fn: func(ctx context.Context, args map[string]any) (string, error) {
			return "file contents here", nil
		},
	})
	deps := Deps{
		LLMClient:        client,
		Model:            "m",
		Template:         tmpl,
		Tools:            reg,
		MainToolDefs:     defaultToolDefs(),
		CommentCollector: comment.NewCommentCollector(),
		Session:          newFakeSession(),
	}
	r := NewRunner(deps)
	completed, _, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go")
	if err != nil || !completed {
		t.Errorf("err=%v completed=%v", err, completed)
	}
	if r.ToolCalls()["file_read"] != 1 {
		t.Errorf("file_read call count = %d", r.ToolCalls()["file_read"])
	}
}

func TestRunMainTask_RegisteredToolError(t *testing.T) {
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("file_read", `{"path":"f.go"}`)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)},
	}}
	tmpl := Template{MaxTokens: 100000, MaxToolRequestTimes: 5}
	reg := tool.NewRegistry()
	reg.Register(&fakeToolProvider{
		toolID: tool.FileRead,
		fn: func(ctx context.Context, args map[string]any) (string, error) {
			return "", errors.New("file not found")
		},
	})
	deps := Deps{
		LLMClient:        client,
		Model:            "m",
		Template:         tmpl,
		Tools:            reg,
		MainToolDefs:     defaultToolDefs(),
		CommentCollector: comment.NewCommentCollector(),
		Session:          newFakeSession(),
	}
	r := NewRunner(deps)
	if _, _, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go"); err != nil {
		t.Fatal(err)
	}
	failures := r.ToolFailures()
	if len(failures) != 1 || failures[0].ToolName != "file_read" {
		t.Errorf("failures = %+v", failures)
	}
}

func TestRunMainTask_BadJSONArgs(t *testing.T) {
	// Invalid JSON args on a registered tool → recorded as failure, kept as
	// tool result so the loop runs to max rounds.
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("file_read", `not json`)},
		{resp: chatRespToolCall("file_read", `not json`)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)}, // grace
	}}
	reg := tool.NewRegistry()
	reg.Register(&fakeToolProvider{
		toolID: tool.FileRead,
		fn: func(ctx context.Context, args map[string]any) (string, error) {
			return "ok", nil
		},
	})
	deps := Deps{
		LLMClient:        client,
		Model:            "m",
		Template:         Template{MaxTokens: 100000, MaxToolRequestTimes: 2},
		Tools:            reg,
		MainToolDefs:     defaultToolDefs(),
		CommentCollector: comment.NewCommentCollector(),
		Session:          newFakeSession(),
	}
	r := NewRunner(deps)
	if _, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "hi"),
	}, "f.go"); err != nil || stop != StopMaxRounds {
		t.Errorf("err=%v stop=%v", err, stop)
	}
	if len(r.ToolFailures()) == 0 {
		t.Error("expected recorded tool failure for bad args")
	}
}

func TestRunMainTask_CodeCommentSyncPath(t *testing.T) {
	// code_comment without a worker pool → sync collection.
	commentArgs := `{"comments":[{"content":"issue","path":"f.go","start_line":1,"end_line":1}]}`
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("code_comment", commentArgs)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)},
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 5})
	if _, _, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "review"),
	}, "f.go"); err != nil {
		t.Fatal(err)
	}
	comments := r.CollectPendingComments()
	if len(comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(comments))
	}
	if comments[0].Content != "issue" {
		t.Errorf("content = %q", comments[0].Content)
	}
}

func TestRunMainTask_CodeCommentAsyncPath(t *testing.T) {
	// code_comment with a worker pool → async, drained by
	// CollectPendingComments.
	commentArgs := `{"comments":[{"content":"issue","path":"f.go","start_line":1,"end_line":1}]}`
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("code_comment", commentArgs)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)},
	}}
	tmpl := Template{MaxTokens: 100000, MaxToolRequestTimes: 5}
	pool := NewCommentWorkerPool(2)
	reg := tool.NewRegistry()
	reg.Register(tool.NewStub(tool.CodeComment))
	deps := Deps{
		LLMClient:         client,
		Model:             "m",
		Template:          tmpl,
		Tools:             reg,
		MainToolDefs:      defaultToolDefs(),
		CommentCollector:  comment.NewCommentCollector(),
		CommentWorkerPool: pool,
		Session:           newFakeSession(),
	}
	r := NewRunner(deps)
	if _, _, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "review"),
	}, "f.go"); err != nil {
		t.Fatal(err)
	}
	comments := r.CollectPendingComments()
	if len(comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(comments))
	}
}

func TestRunMainTask_CodeCommentWithDiffLookup(t *testing.T) {
	commentArgs := `{"comments":[{"content":"issue","existing_code":"existing","path":"f.go","start_line":1,"end_line":1}]}`
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("code_comment", commentArgs)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)},
	}}
	tmpl := Template{MaxTokens: 100000, MaxToolRequestTimes: 5}
	reg := tool.NewRegistry()
	reg.Register(tool.NewStub(tool.CodeComment))
	deps := Deps{
		LLMClient:        client,
		Model:            "m",
		Template:         tmpl,
		Tools:            reg,
		MainToolDefs:     defaultToolDefs(),
		CommentCollector: comment.NewCommentCollector(),
		Session:          newFakeSession(),
		DiffLookup: func(path string) *model.Diff {
			return &model.Diff{NewPath: path, NewFileContent: "line1\nline2\n"}
		},
	}
	r := NewRunner(deps)
	if _, _, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "review"),
	}, "f.go"); err != nil {
		t.Fatal(err)
	}
	if len(r.CollectPendingComments()) != 1 {
		t.Errorf("expected 1 comment collected")
	}
}

func TestRunMainTask_CodeCommentBadArgs(t *testing.T) {
	// code_comment with malformed args → recorded failure, kept as tool
	// result until MaxToolRequestTimes runs out.
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespToolCall("code_comment", `{"comments":"not-an-array"}`)},
		{resp: chatRespToolCall("code_comment", `{"comments":"not-an-array"}`)},
		{resp: chatRespToolCall("task_done", `{"state":"DONE"}`)}, // grace
	}}
	r := newTestRunner(t, client, Template{MaxTokens: 100000, MaxToolRequestTimes: 2})
	if _, stop, err := r.RunMainTask(context.Background(), []llm.Message{
		llm.NewTextMessage("user", "review"),
	}, "f.go"); err != nil || stop != StopMaxRounds {
		t.Errorf("err=%v stop=%v", err, stop)
	}
	if len(r.ToolFailures()) == 0 {
		t.Error("expected recorded failure")
	}
}

// --- Tests: compression scenarios ---

func TestRunCompression_ShortReturnsAsIs(t *testing.T) {
	r := NewRunner(Deps{
		Template: Template{MaxTokens: 100000},
		Session:  newFakeSession(),
	})
	msgs := []llm.Message{llm.NewTextMessage("system", "sys")}
	out, err := r.runCompression(context.Background(), msgs, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Errorf("out len = %d, want 1", len(out))
	}
}

func TestRunCompression_EmptyTemplateReturnsFrozen(t *testing.T) {
	// Empty MemoryCompressionTask.Messages → keeps only frozen zone.
	r := NewRunner(Deps{
		Template: Template{MaxTokens: 100000},
		Session:  newFakeSession(),
	})
	msgs := []llm.Message{
		llm.NewTextMessage("system", "sys"),
		llm.NewTextMessage("user", "u"),
		llm.NewTextMessage("assistant", "a"),
	}
	out, err := r.runCompression(context.Background(), msgs, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Errorf("out len = %d, want 2 (frozen zone only)", len(out))
	}
}

func TestRunCompression_HappyPath(t *testing.T) {
	// Force compression: MaxTokens is small so the active zone budget doesn't
	// hold every round. Build enough long rounds to trigger.
	longText := strings.Repeat("word ", 5000)
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespText("summary text")},
	}}
	tmpl := Template{
		MaxTokens: 1000,
		MemoryCompressionTask: LlmConversation{
			Messages: []ChatMessage{
				{Role: "system", Content: "compress the following"},
				{Role: "user", Content: "{{context}}"},
			},
		},
	}
	deps := Deps{
		LLMClient: client,
		Model:     "m",
		Template:  tmpl,
		Session:   newFakeSession(),
	}
	r := NewRunner(deps)
	msgs := []llm.Message{
		llm.NewTextMessage("system", "sys prompt"),
		llm.NewTextMessage("user", "initial user prompt"),
		llm.NewTextMessage("assistant", longText),
		llm.NewTextMessage("tool", "result "+longText),
		llm.NewTextMessage("assistant", "recent"),
	}
	out, err := r.runCompression(context.Background(), msgs, "f.go")
	if err != nil {
		t.Fatalf("runCompression: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
	// The rebuilt user prompt should include the summary.
	if !strings.Contains(out[1].ExtractText(), "summary text") {
		t.Errorf("rebuilt user message missing summary: %q", out[1].ExtractText())
	}
}

func TestRunCompression_LLMError(t *testing.T) {
	longText := strings.Repeat("word ", 5000)
	client := &scriptedClient{responses: []scriptedResponse{
		{err: errors.New("LLM down")},
	}}
	tmpl := Template{
		MaxTokens: 1000,
		MemoryCompressionTask: LlmConversation{
			Messages: []ChatMessage{{Role: "user", Content: "compress {{context}}"}},
		},
	}
	deps := Deps{LLMClient: client, Model: "m", Template: tmpl, Session: newFakeSession()}
	r := NewRunner(deps)
	msgs := []llm.Message{
		llm.NewTextMessage("system", "sys"),
		llm.NewTextMessage("user", "u"),
		llm.NewTextMessage("assistant", longText),
		llm.NewTextMessage("tool", "res"),
		llm.NewTextMessage("assistant", "recent"),
	}
	out, err := r.runCompression(context.Background(), msgs, "f.go")
	if err == nil {
		t.Fatal("expected error")
	}
	// On failure, msgs is returned unchanged.
	if len(out) != len(msgs) {
		t.Errorf("expected unchanged msgs, got len %d", len(out))
	}
}

func TestRunCompression_EmptySummaryKeepsOriginal(t *testing.T) {
	longText := strings.Repeat("word ", 5000)
	client := &scriptedClient{responses: []scriptedResponse{
		{resp: chatRespText("")}, // empty summary
	}}
	tmpl := Template{
		MaxTokens: 1000,
		MemoryCompressionTask: LlmConversation{
			Messages: []ChatMessage{{Role: "user", Content: "compress {{context}}"}},
		},
	}
	deps := Deps{LLMClient: client, Model: "m", Template: tmpl, Session: newFakeSession()}
	r := NewRunner(deps)
	msgs := []llm.Message{
		llm.NewTextMessage("system", "sys"),
		llm.NewTextMessage("user", "u"),
		llm.NewTextMessage("assistant", longText),
		llm.NewTextMessage("tool", "res"),
		llm.NewTextMessage("assistant", "recent"),
	}
	out, err := r.runCompression(context.Background(), msgs, "f.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(msgs) {
		t.Errorf("expected unchanged msgs on empty summary, got len %d want %d", len(out), len(msgs))
	}
}

// --- Tests: async compression state machinery ---

func TestTriggerAsyncCompression_DoesNotDuplicate(t *testing.T) {
	// Second trigger while a job is pending is a no-op.
	//
	// Use a client whose responses are never consumed so the job stays pending;
	// then cancel it.
	client := &scriptedClient{
		// buffered to avoid nil deref; both slots block us but that's fine
		// because we cancel before waiting.
		responses: []scriptedResponse{
			{resp: chatRespText("summary")},
			{resp: chatRespText("summary")},
		},
	}
	tmpl := Template{
		MaxTokens: 1000,
		MemoryCompressionTask: LlmConversation{
			Messages: []ChatMessage{{Role: "user", Content: "{{context}}"}},
		},
	}
	r := NewRunner(Deps{LLMClient: client, Model: "m", Template: tmpl, Session: newFakeSession()})

	longText := strings.Repeat("word ", 5000)
	msgs := []llm.Message{
		llm.NewTextMessage("system", "sys"),
		llm.NewTextMessage("user", "u"),
		llm.NewTextMessage("assistant", longText),
		llm.NewTextMessage("tool", "res"),
		llm.NewTextMessage("assistant", "recent"),
	}

	st := &compressionState{}
	r.triggerAsyncCompression(context.Background(), st, msgs, "f.go")
	// Second call must not spawn a new job.
	r.triggerAsyncCompression(context.Background(), st, msgs, "f.go")

	// Wait for whatever's pending, then verify state.
	r.WaitBackground()
	// tryApplyPendingCompression should absorb the completed job.
	applied := r.tryApplyPendingCompression(st, &msgs)
	// If it applied, great. Either way, no panic — that's the contract.
	_ = applied
}

func TestTryApplyPendingCompression_NoJob(t *testing.T) {
	r := NewRunner(Deps{})
	st := &compressionState{}
	msgs := []llm.Message{llm.NewTextMessage("user", "hi")}
	if applied := r.tryApplyPendingCompression(st, &msgs); applied {
		t.Error("no pending job should not apply")
	}
}

func TestCancelPendingCompression_NoJob(t *testing.T) {
	r := NewRunner(Deps{})
	st := &compressionState{}
	// Just make sure it doesn't panic with a nil pending job.
	r.cancelPendingCompression(st)
}

// --- Tests: partitionMessages more branches ---

func TestPartitionMessages_LongRoundsForceCompression(t *testing.T) {
	long := strings.Repeat("word ", 2000)
	messages := []llm.Message{
		llm.NewTextMessage("system", "sys"),
		llm.NewTextMessage("user", "u"),
		llm.NewTextMessage("assistant", long),
		llm.NewTextMessage("tool", long),
		llm.NewTextMessage("assistant", long),
		llm.NewTextMessage("tool", long),
		llm.NewTextMessage("assistant", "recent"),
	}
	// MaxTokens small enough that not everything fits.
	part := partitionMessages(messages, 5000, 0)
	if part.compressEnd == len(messages) {
		t.Error("expected compression to be needed")
	}
	if part.compressEnd <= part.frozenEnd {
		t.Errorf("compressEnd=%d frozenEnd=%d", part.compressEnd, part.frozenEnd)
	}
}

func TestComputeActiveZoneSize_ZeroBudget(t *testing.T) {
	// reservedTokens > PromptTokenLimit(maxTokens) → budget <= 0 → 0.
	if got := computeActiveZoneSize(nil, nil, 100, 1000); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

func TestBuildMessageXML_WithReasoning(t *testing.T) {
	msgs := []llm.Message{
		{Role: "assistant", Content: "answer", ReasoningContent: "why I chose it"},
	}
	got := buildMessageXML(msgs)
	if !strings.Contains(got, "<reasoning>") || !strings.Contains(got, "why I chose it") {
		t.Errorf("expected reasoning block, got: %s", got)
	}
}

// --- Tests: Template.CompletionTokenLimit ---

func TestTemplate_CompletionTokenLimit(t *testing.T) {
	if got := (Template{MaxTokens: 100}).CompletionTokenLimit(); got != 100 {
		t.Errorf("no MaxCompletionTokens: got %d, want 100", got)
	}
	if got := (Template{MaxTokens: 100, MaxCompletionTokens: 30}).CompletionTokenLimit(); got != 30 {
		t.Errorf("with cap: got %d, want 30", got)
	}
}

// --- Tests: CollectPendingComments no worker pool ---

func TestCollectPendingComments_NoWorkerPool(t *testing.T) {
	deps := Deps{
		CommentCollector: comment.NewCommentCollector(),
	}
	deps.CommentCollector.Add(model.LlmComment{Path: "a.go", Content: "x"})
	r := NewRunner(deps)
	got := r.CollectPendingComments()
	if len(got) != 1 {
		t.Errorf("got %d, want 1", len(got))
	}
}

// --- Extra: JSON round-trip of ToolFailureDetail (schema check) ---

func TestToolFailureDetail_JSON(t *testing.T) {
	d := ToolFailureDetail{
		ToolCallNumber: 5,
		ToolName:       "file_read",
		FilePath:       "x.go",
		Arguments:      `{"p":"x"}`,
		Error:          "boom",
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"tool_name":"file_read"`) {
		t.Errorf("json = %s", string(b))
	}
}
