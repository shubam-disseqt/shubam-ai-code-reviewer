// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llmloop"
)

func TestFileSession_ConcurrentAppendNoInterleave(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, "sid-concurrent")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fs := s.GetOrCreateFileSession("f")

	const goroutines = 16
	const perG = 8
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				fs.AppendTaskRecord(llmloop.MainTask, []llm.Message{{Role: "user", Content: "hi"}})
			}
		}()
	}
	wg.Wait()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Every line must parse cleanly — proof that concurrent writers did not
	// interleave bytes mid-record.
	raw, err := os.ReadFile(filepath.Join(dir, "sid-concurrent.jsonl"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	llmRequestCount := 0
	for i, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d unparseable (interleaved write?): %v\nline: %q", i, err, line)
		}
		if rec["type"] == "llm_request" {
			llmRequestCount++
		}
	}
	want := goroutines * perG
	if llmRequestCount != want {
		t.Fatalf("llm_request count = %d, want %d", llmRequestCount, want)
	}
}

func TestFileSession_RequestNoIncrementsPerTaskType(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, "sid-reqno")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fs := s.GetOrCreateFileSession("f")

	fs.AppendTaskRecord(llmloop.MainTask, nil)
	fs.AppendTaskRecord(llmloop.MainTask, nil)
	fs.AppendTaskRecord(llmloop.MemoryCompressionTask, nil)
	fs.AppendTaskRecord(llmloop.MainTask, nil)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := readJSONL(t, filepath.Join(dir, "sid-reqno.jsonl"))
	var mainNos, memNos []float64
	for _, rec := range records {
		if rec["type"] != "llm_request" {
			continue
		}
		no, _ := rec["request_no"].(float64)
		switch rec["taskType"] {
		case string(llmloop.MainTask):
			mainNos = append(mainNos, no)
		case string(llmloop.MemoryCompressionTask):
			memNos = append(memNos, no)
		}
	}
	if want := []float64{1, 2, 3}; !equalFloats(mainNos, want) {
		t.Fatalf("MainTask request_no = %v, want %v", mainNos, want)
	}
	if want := []float64{1}; !equalFloats(memNos, want) {
		t.Fatalf("MemoryCompressionTask request_no = %v, want %v", memNos, want)
	}
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
