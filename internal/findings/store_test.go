// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package findings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := Load(dir, "o", "r", 1)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty slice, got %d", len(got))
	}
}

func TestSaveThenLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	in := []Finding{
		{
			Fingerprint: "abc123",
			Comment:     model.LlmComment{Path: "foo.go", Content: "x", StartLine: 1, EndLine: 1},
			State:       StateNew,
			HeadSHA:     "deadbeef",
		},
	}
	if err := Save(dir, "o", "r", 42, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir, "o", "r", 42)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 || got[0].Fingerprint != "abc123" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if got[0].Comment.Path != "foo.go" {
		t.Errorf("nested comment lost: %+v", got[0].Comment)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	// After Save() completes, no lingering .tmp should exist next to the
	// final file.
	dir := t.TempDir()
	if err := Save(dir, "o", "r", 1, []Finding{{Fingerprint: "x"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("stray tmp file left behind: %s", e.Name())
		}
	}
}

func TestSaveOverwritesPrevious(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, "o", "r", 1, []Finding{{Fingerprint: "a"}, {Fingerprint: "b"}}); err != nil {
		t.Fatalf("save1: %v", err)
	}
	if err := Save(dir, "o", "r", 1, []Finding{{Fingerprint: "c"}}); err != nil {
		t.Fatalf("save2: %v", err)
	}
	got, err := Load(dir, "o", "r", 1)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 || got[0].Fingerprint != "c" {
		t.Errorf("expected overwrite, got %+v", got)
	}
}

func TestLoadInvalidJSONErrors(t *testing.T) {
	dir := t.TempDir()
	// Handcraft a bad file at the expected path.
	path := filepath.Join(dir, "o_r_1.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write bad: %v", err)
	}
	if _, err := Load(dir, "o", "r", 1); err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
}

func TestDefaultDirRespectsEnv(t *testing.T) {
	t.Setenv("SACR_FINDINGS_DIR", "/tmp/sacr-findings-test-xyz")
	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if got != "/tmp/sacr-findings-test-xyz" {
		t.Errorf("env override ignored: %q", got)
	}
}

func TestSaveErrorsOnUnwritableDir(t *testing.T) {
	// Point Save at a path that can't be created (a file, not a dir).
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	target := filepath.Join(blocker, "nested")
	err := Save(target, "o", "r", 1, []Finding{{Fingerprint: "a"}})
	if err == nil {
		t.Fatal("expected mkdir failure, got nil")
	}
}

func TestSaveCreatesDirIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	if err := Save(dir, "o", "r", 1, []Finding{{Fingerprint: "a"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir, "o", "r", 1)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 finding, got %d", len(got))
	}
}

func TestDefaultDirFallsBackToHome(t *testing.T) {
	t.Setenv("SACR_FINDINGS_DIR", "")
	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("want absolute path, got %q", got)
	}
	if filepath.Base(got) != "findings" {
		t.Errorf("want .../findings, got %q", got)
	}
}
