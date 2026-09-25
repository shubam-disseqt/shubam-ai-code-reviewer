// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/filetype"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/index"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/logutil"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// testLogger returns a text-mode logger writing to buf so substring
// assertions on stage-prefixed lines keep working.
func testLogger(buf *bytes.Buffer) *slog.Logger {
	return logutil.New(buf, logutil.FormatText, slog.LevelDebug)
}

// newTestStore returns a fresh in-memory store, optionally pre-populated with
// rows for the listed paths.
func newTestStore(t *testing.T, seeded ...string) index.Store {
	t.Helper()
	store, err := index.NewStore(context.Background(), "sqlite:///:memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, p := range seeded {
		if err := store.UpsertSummary(context.Background(), index.FileSummary{
			Path:        p,
			Language:    filetype.LangGo,
			Summary:     "seed",
			ContentHash: "h_" + p,
			LOC:         1,
			UpdatedAt:   time.Unix(0, 0),
		}); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}
	return store
}

func TestMissingSummaryPathsAllPresent(t *testing.T) {
	store := newTestStore(t, "a.go", "b.go", "c.go")
	got, err := missingSummaryPaths(context.Background(), store, []string{"a.go", "b.go"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestMissingSummaryPathsPartial(t *testing.T) {
	store := newTestStore(t, "a.go", "c.go")
	got, err := missingSummaryPaths(context.Background(), store, []string{"a.go", "b.go", "c.go", "d.go"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	sort.Strings(got)
	want := []string{"b.go", "d.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestMissingSummaryPathsNilStoreOrEmpty(t *testing.T) {
	if got, _ := missingSummaryPaths(context.Background(), nil, []string{"a.go"}); got != nil {
		t.Errorf("nil store: expected nil, got %v", got)
	}
	if got, _ := missingSummaryPaths(context.Background(), newTestStore(t), nil); got != nil {
		t.Errorf("empty changed: expected nil, got %v", got)
	}
}

// writeRepoFile creates repo/rel with enough bytes to be summarizable.
func writeRepoFile(t *testing.T, repo, rel string) {
	t.Helper()
	body := "package x\n\n" + strings.Repeat("// padding so the file clears the trivial threshold\n", 20)
	if err := os.WriteFile(filepath.Join(repo, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexMissingSummarizesUnseenFilesOnly(t *testing.T) {
	repo := t.TempDir()
	writeRepoFile(t, repo, "seen.go")
	writeRepoFile(t, repo, "new.go")
	store := newTestStore(t, "seen.go")
	stub := &stubLLM{response: `{"files":[{"path":"new.go","language":"go","summary":"stubbed"}]}`}
	kept := []model.Diff{
		{NewPath: "seen.go"},
		{NewPath: "new.go"},
		{OldPath: "gone.go", NewPath: "/dev/null", IsDeleted: true},
	}
	var buf bytes.Buffer

	indexMissing(context.Background(), store, stub, "cheap-model", repo, kept, nil, testLogger(&buf))

	if stub.lastReq.Model != "cheap-model" {
		t.Errorf("summaries must run on the cheap tier, got model %q", stub.lastReq.Model)
	}
	sys := ""
	for _, m := range stub.lastReq.Messages {
		if m.Role == "system" {
			sys = m.ExtractText()
		}
	}
	if !strings.Contains(sys, "new.go") || strings.Contains(sys, "seen.go") || strings.Contains(sys, "gone.go") {
		t.Errorf("prompt should carry only the unseen live file, got:\n%s", sys)
	}
	if _, err := store.GetSummary(context.Background(), "new.go"); err != nil {
		t.Errorf("new.go should be indexed after the stage: %v", err)
	}
	if !strings.Contains(buf.String(), "indexed 1 file(s)") {
		t.Errorf("expected indexed log line, got %q", buf.String())
	}
}

func TestIndexMissingNoopWhenAllPresent(t *testing.T) {
	store := newTestStore(t, "a.go")
	stub := &stubLLM{err: errors.New("must not be called")}
	var buf bytes.Buffer

	indexMissing(context.Background(), store, stub, "m", t.TempDir(), []model.Diff{{NewPath: "a.go"}}, nil, testLogger(&buf))

	if len(stub.lastReq.Messages) != 0 {
		t.Error("LLM called although every file already had a summary")
	}
	if buf.Len() != 0 {
		t.Errorf("expected silence, got %q", buf.String())
	}
}

func TestIndexMissingLLMFailureIsNonFatal(t *testing.T) {
	repo := t.TempDir()
	writeRepoFile(t, repo, "new.go")
	stub := &stubLLM{err: errors.New("provider down")}
	var buf bytes.Buffer

	indexMissing(context.Background(), newTestStore(t), stub, "m", repo, []model.Diff{{NewPath: "new.go"}}, nil, testLogger(&buf))

	if !strings.Contains(buf.String(), "indexed 0 file(s)") {
		t.Errorf("stage must report and continue, got %q", buf.String())
	}
}

func TestIndexMissingIndexesExtraImporters(t *testing.T) {
	repo := t.TempDir()
	writeRepoFile(t, repo, "caller.go")
	store := newTestStore(t, "changed.go")
	stub := &stubLLM{response: `{"files":[{"path":"caller.go","language":"go","summary":"stubbed"}]}`}
	var buf bytes.Buffer

	indexMissing(context.Background(), store, stub, "m", repo, []model.Diff{{NewPath: "changed.go"}}, []string{"caller.go"}, testLogger(&buf))

	if _, err := store.GetSummary(context.Background(), "caller.go"); err != nil {
		t.Errorf("importer should be indexed alongside the diff: %v", err)
	}
}
