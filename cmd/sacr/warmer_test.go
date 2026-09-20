// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/filetype"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/index"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/logutil"
)

// testLogger returns a text-mode logger writing to buf so existing substring
// assertions ("warmer:", "1 new", ...) keep working under the slog migration.
func testLogger(buf *bytes.Buffer) *slog.Logger {
	return logutil.New(buf, logutil.FormatText, slog.LevelDebug)
}

// helper: fresh in-memory store, optionally pre-populated with rows for the
// listed paths.
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

func TestMissingSummaryPathsAllMissing(t *testing.T) {
	store := newTestStore(t)
	got, err := missingSummaryPaths(context.Background(), store, []string{"x.go", "y.go"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	sort.Strings(got)
	want := []string{"x.go", "y.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
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

func TestMissingSummaryPathsNilStore(t *testing.T) {
	got, err := missingSummaryPaths(context.Background(), nil, []string{"a.go"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestMissingSummaryPathsEmptyChanged(t *testing.T) {
	store := newTestStore(t)
	got, err := missingSummaryPaths(context.Background(), store, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestWarmerLogDirRespectsSessionOverride(t *testing.T) {
	tmp := t.TempDir()
	sessionDir := filepath.Join(tmp, "sessions")
	t.Setenv("SACR_SESSION_DIR", sessionDir)
	got, err := warmerLogDir()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Warmer log dir sits alongside the sessions dir — i.e. its parent.
	if got != tmp {
		t.Errorf("got %q want %q", got, tmp)
	}
}

func TestWarmerLogDirFallsBackToHome(t *testing.T) {
	t.Setenv("SACR_SESSION_DIR", "")
	got, err := warmerLogDir()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".sacr")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSpawnIndexWarmerNoMissingIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	spawnIndexWarmer("/tmp/anywhere", nil, testLogger(&buf))
	if buf.Len() != 0 {
		t.Errorf("expected silent no-op, got %q", buf.String())
	}
}

// TestSpawnIndexWarmerLogsPid verifies that when missing paths are present
// and we can resolve the current executable, we announce the spawn and hand
// off to a real subprocess. We route stdout to a temp log dir and assert on
// the launcher's log line, not the child's behavior — the child immediately
// exits because the test harness lacks SACR_DB_URL, which is the whole
// point of the "best-effort, review continues" contract.
func TestSpawnIndexWarmerLogsPidAndPath(t *testing.T) {
	if _, err := os.Executable(); err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	// The spawned warmer child inherits the log file handle. On Windows
	// t.TempDir cleanup fails ("The process cannot access the file
	// because it is being used by another process") because the child
	// may not have exited yet when the deferred RemoveAll runs. Skip
	// here rather than layer subprocess-wait complexity into the test —
	// the warmer's fire-and-forget contract is exercised on unix CI.
	if runtime.GOOS == "windows" {
		t.Skip("warmer subprocess file-handle inheritance breaks Windows temp-dir cleanup")
	}
	tmp := t.TempDir()
	t.Setenv("SACR_SESSION_DIR", filepath.Join(tmp, "sessions"))
	// The child inherits our env. Ensure it fails fast rather than talking
	// to a real store or LLM.
	t.Setenv("SACR_DB_URL", "")

	var buf bytes.Buffer
	spawnIndexWarmer(tmp, []string{"a.go", "b.go"}, testLogger(&buf))

	msg := buf.String()
	if !strings.Contains(msg, "warmer:") {
		t.Fatalf("expected warmer log line, got %q", msg)
	}
	if !strings.Contains(msg, "2 file(s) missing") {
		t.Errorf("expected count in log, got %q", msg)
	}
	// Legacy grep pattern "pid <N>" is preserved by the slog line message;
	// the structured attr also ships as pid=<N>. Accept either.
	if !strings.Contains(msg, "pid ") && !strings.Contains(msg, "pid=") {
		t.Errorf("expected pid in log, got %q", msg)
	}
	if !strings.Contains(msg, filepath.Join(tmp, "warmer.log")) {
		t.Errorf("expected log path in message, got %q", msg)
	}
}
