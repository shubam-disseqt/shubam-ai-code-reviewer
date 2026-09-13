// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package index

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
)

// fakeLLM returns a canned summarize response echoing the paths in the
// prompt. Enough to exercise the pipeline without any network.
type fakeLLM struct {
	calls int32
	// failEvery makes every Nth call return an error; N=0 disables.
	failEvery int32
}

func (f *fakeLLM) CompletionsWithCtx(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	n := atomic.AddInt32(&f.calls, 1)
	if f.failEvery > 0 && n%f.failEvery == 0 {
		return nil, errors.New("fake llm: transient failure")
	}
	// Pull `### path` markers out of the system prompt.
	sys := ""
	for _, m := range req.Messages {
		if m.Role == "system" {
			sys = m.ExtractText()
			break
		}
	}
	paths := extractPromptPaths(sys)
	env := summarizeEnvelope{}
	for _, p := range paths {
		env.Files = append(env.Files, summarizeFileJSON{
			Path:     p,
			Language: "go",
			Summary:  "auto: " + p,
			Symbols:  []summarizeSymbol{{Name: "F", Kind: "function"}},
		})
	}
	body, _ := json.Marshal(env)
	s := string(body)
	return &llm.ChatResponse{
		Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant", Content: &s}}},
	}, nil
}

func extractPromptPaths(prompt string) []string {
	var out []string
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "### ") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "### ")))
		}
	}
	return out
}

// scratchRepo builds a temp dir with the given files and returns its root.
func scratchRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestIndexRepoHappyPath(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go":     strings.Repeat("package a\n// line\n", 100), // >600B, <5000B → normal batch
		"b.go":     strings.Repeat("package b\n// line\n", 100),
		"tiny.go":  "package t\n", // <600B → trivial bucket
		"skip.svg": "<svg/>",      // skip pattern
	})

	store := openTestStore(t)
	fake := &fakeLLM{}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test"})

	n, err := idx.IndexRepo(context.Background(), root)
	if err != nil {
		t.Fatalf("IndexRepo: %v", err)
	}
	if n != 3 {
		t.Errorf("indexed = %d, want 3 (a, b, tiny)", n)
	}

	// tiny.go should have empty summary from the trivial path.
	tiny, err := store.GetSummary(context.Background(), "tiny.go")
	if err != nil {
		t.Fatal(err)
	}
	if tiny.Summary != "" {
		t.Errorf("trivial file should have empty summary: %q", tiny.Summary)
	}
	if tiny.ContentHash == "" {
		t.Errorf("trivial file should still have content hash")
	}

	// a.go should have LLM summary.
	a, err := store.GetSummary(context.Background(), "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.Summary, "auto: ") {
		t.Errorf("a.go summary not set: %q", a.Summary)
	}
}

func TestIndexRepoContentHashSkip(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go": strings.Repeat("package a\n// line\n", 100),
	})
	store := openTestStore(t)
	fake := &fakeLLM{}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test"})

	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	firstCalls := atomic.LoadInt32(&fake.calls)

	// Second run — same content, hash guard should skip the LLM call.
	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&fake.calls); got != firstCalls {
		t.Errorf("expected 0 new LLM calls on unchanged repo, got %d (was %d)", got-firstCalls, firstCalls)
	}
}

func TestIndexRepoFullBypassesHash(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go": strings.Repeat("package a\n// line\n", 100),
	})
	store := openTestStore(t)
	fake := &fakeLLM{}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test", Full: true})

	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	firstCalls := atomic.LoadInt32(&fake.calls)
	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&fake.calls) <= firstCalls {
		t.Errorf("--full should re-index: calls=%d", fake.calls)
	}
}

func TestIndexRepoRemovesDeletedFiles(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go": strings.Repeat("package a\n// line\n", 100),
		"b.go": strings.Repeat("package b\n// line\n", 100),
	})
	store := openTestStore(t)
	fake := &fakeLLM{}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test"})

	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	// Remove b.go from the working tree and re-index.
	if err := os.Remove(filepath.Join(root, "b.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	paths, err := store.ListAllPaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if p == "b.go" {
			t.Errorf("b.go should have been removed after deletion")
		}
	}
}

func TestIndexRepoBadBatchDoesNotAbort(t *testing.T) {
	t.Parallel()
	// Make every 2nd call fail. With buildBatches producing multiple batches,
	// at least one should still succeed.
	root := scratchRepo(t, map[string]string{
		"a.go": strings.Repeat("package a\n// line\n", 500), // large → solo batch
		"b.go": strings.Repeat("package b\n// line\n", 500), // large → solo batch
		"c.go": strings.Repeat("package c\n// line\n", 500), // large → solo batch
		"d.go": strings.Repeat("package d\n// line\n", 500), // large → solo batch
	})
	store := openTestStore(t)
	fake := &fakeLLM{failEvery: 2}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test"})

	n, err := idx.IndexRepo(context.Background(), root)
	if err != nil {
		t.Fatalf("bad batch should not surface: %v", err)
	}
	if n == 0 {
		t.Errorf("expected some files indexed despite failures, got %d", n)
	}
}

func TestIndexDiffOnlyChanged(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go": strings.Repeat("package a\n// line\n", 100),
		"b.go": strings.Repeat("package b\n// line\n", 100),
	})
	store := openTestStore(t)
	fake := &fakeLLM{}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test"})

	n, err := idx.IndexDiff(context.Background(), root, []string{"a.go"})
	if err != nil {
		t.Fatalf("IndexDiff: %v", err)
	}
	if n != 1 {
		t.Errorf("IndexDiff should re-index only a.go, got n=%d", n)
	}
	if _, err := store.GetSummary(context.Background(), "a.go"); err != nil {
		t.Errorf("a.go not persisted: %v", err)
	}
	// b.go was not in the diff and hasn't been indexed before.
	_, err = store.GetSummary(context.Background(), "b.go")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("b.go should not be indexed via IndexDiff")
	}
}

func TestIndexRepoStatusSnapshot(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go":    strings.Repeat("package a\n// line\n", 100),
		"tiny.go": "package t\n",
	})
	store := openTestStore(t)
	fake := &fakeLLM{}
	status := &Status{}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test", Status: status})

	if _, err := idx.IndexRepo(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	snap := status.Snapshot()
	if snap.Done != 2 || snap.Total != 2 {
		t.Errorf("status snapshot = %+v, want Total=2 Done=2", snap)
	}
	if snap.EndedAt.IsZero() {
		t.Errorf("EndedAt should be set after Finish")
	}
}

func TestIndexRepoContextCancel(t *testing.T) {
	t.Parallel()
	root := scratchRepo(t, map[string]string{
		"a.go": strings.Repeat("package a\n// line\n", 100),
	})
	store := openTestStore(t)
	fake := &fakeLLM{}
	idx := NewIndexer(store, fake, IndexerOptions{Model: "test"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before we even start
	_, err := idx.IndexRepo(ctx, root)
	if err == nil {
		t.Errorf("cancelled context should surface an error")
	}
}
