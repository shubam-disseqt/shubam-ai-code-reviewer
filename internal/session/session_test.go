// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llmloop"
)

func TestNew_InvalidDirReturnsError(t *testing.T) {
	// A file — not a directory — at the target path forces MkdirAll to fail.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := New(filepath.Join(blocker, "child"), "sid")
	if err == nil {
		t.Fatal("expected error when dir path traverses a file")
	}
}

func TestNew_EmptySessionIDGetsUUID(t *testing.T) {
	s, err := New(t.TempDir(), "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	if s.SessionID() == "" {
		t.Fatal("expected generated session ID, got empty")
	}
}

func TestSession_RecordChainAndClose(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, "sid-chain")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fs := s.GetOrCreateFileSession("file.go")
	tr := fs.AppendTaskRecord(llmloop.MainTask, []llm.Message{{Role: "user", Content: "hi"}})

	content := "hello"
	tr.SetResponse(&llm.ChatResponse{
		Model: "test-model",
		Choices: []llm.Choice{{
			Message: llm.ResponseMessage{Role: "assistant", Content: &content},
		}},
		Usage: &llm.UsageInfo{PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4},
	}, 20*time.Millisecond)

	tr.AddToolResult("tool_a", `{"x":1}`, "ok")
	tr.AddToolFailure("tool_b", `{"y":2}`, "boom", 5*time.Millisecond)

	tr2 := fs.AppendTaskRecord(llmloop.MemoryCompressionTask, nil)
	tr2.SetError(errors.New("nope"), 7*time.Millisecond)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent.
	if err := s.Close(); err != nil {
		t.Fatalf("Close again: %v", err)
	}

	records := readJSONL(t, filepath.Join(dir, "sid-chain.jsonl"))
	if len(records) < 7 {
		t.Fatalf("want at least 7 records, got %d", len(records))
	}

	// First is session_start with no parent; each subsequent record chains to
	// the previous one's UUID.
	if records[0]["type"] != "session_start" {
		t.Fatalf("first record type = %v, want session_start", records[0]["type"])
	}
	if p, _ := records[0]["parentUuid"].(string); p != "" {
		t.Fatalf("session_start parentUuid = %q, want empty", p)
	}
	for i := 1; i < len(records); i++ {
		prevUUID, _ := records[i-1]["uuid"].(string)
		parent, _ := records[i]["parentUuid"].(string)
		if parent != prevUUID {
			t.Fatalf("record %d parentUuid = %q, want %q", i, parent, prevUUID)
		}
	}
	if records[len(records)-1]["type"] != "session_end" {
		t.Fatalf("last record type = %v, want session_end", records[len(records)-1]["type"])
	}

	// LLMFailures reflects the one SetError call.
	end := records[len(records)-1]
	if got, _ := end["llm_failures"].(float64); got != 1 {
		t.Fatalf("llm_failures = %v, want 1", got)
	}
}

func TestSession_SetResponseNilDoesNotPanic(t *testing.T) {
	s, err := New(t.TempDir(), "sid-nil")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	fs := s.GetOrCreateFileSession("f")
	tr := fs.AppendTaskRecord(llmloop.MainTask, nil)
	// Neither of these should panic.
	tr.SetResponse(nil, 0)
	tr.SetResponse(&llm.ChatResponse{}, 0) // empty Choices
}

func TestSession_GetOrCreateFileSession_IdentityPreserving(t *testing.T) {
	s, err := New(t.TempDir(), "sid-id")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	const key = "path/to/thing"
	const N = 32
	seen := make([]llmloop.FileSession, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			seen[i] = s.GetOrCreateFileSession(key)
		}(i)
	}
	wg.Wait()
	first := seen[0]
	for i, fs := range seen {
		if fs != first {
			t.Fatalf("goroutine %d got a different FileSession instance", i)
		}
	}
}

func TestSession_LoadResumeStateStub(t *testing.T) {
	// The resume loader currently only recognizes review_item_done /
	// review_item_reused, which the writer does not emit. So a fresh session
	// file should produce an empty index — this pins the stub behavior.
	dir := t.TempDir()
	s, err := New(dir, "sid-resume")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fs := s.GetOrCreateFileSession("f")
	fs.AppendTaskRecord(llmloop.MainTask, nil)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rs, err := LoadResumeState(filepath.Join(dir, "sid-resume.jsonl"))
	if err != nil {
		t.Fatalf("LoadResumeState: %v", err)
	}
	if len(rs.Items) != 0 {
		t.Fatalf("expected 0 resumed items, got %d", len(rs.Items))
	}
}

func TestLoadResumeState_TolerantOfGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	body := strings.Join([]string{
		`{"type":"session_start","uuid":"a"}`,
		`{not valid json`,
		`{"type":"review_item_done","fingerprint":"fp1","comments":"c1"}`,
		`{"type":"review_item_reused","fingerprint":"fp2","comments":"c2"}`,
		`{"type":"review_item_done","fingerprint":""}`, // empty fp ignored
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	rs, err := LoadResumeState(path)
	if err != nil {
		t.Fatalf("LoadResumeState: %v", err)
	}
	if len(rs.Items) != 2 {
		t.Fatalf("want 2 items, got %d", len(rs.Items))
	}
	if rs.Items["fp1"].Comments != "c1" {
		t.Fatalf("fp1 comments = %q", rs.Items["fp1"].Comments)
	}
}

func TestLoadResumeState_MissingFile(t *testing.T) {
	_, err := LoadResumeState(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err == nil {
		t.Fatal("expected error on missing file")
	}
}

// readJSONL loads a JSONL file as a slice of decoded records. Fails the test
// on any read or parse error — for the tests that use it, malformed lines
// mean the writer is broken.
func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var out []map[string]any
	scanner := bufio.NewScanner(f)
	// Session records can be larger than the default 64k buffer if a request
	// carries a big message history.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("parse %q: %v", scanner.Text(), err)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return out
}
