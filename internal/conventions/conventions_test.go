// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package conventions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates parent dirs and writes a file. Fatal on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoad_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}

func TestLoad_TopLevelOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "# Rules\nUse tabs.\n")
	writeFile(t, filepath.Join(dir, "CONTRIBUTING.md"), "# Style\nPrefer stdlib.\n")

	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "### From `AGENTS.md`") {
		t.Errorf("missing AGENTS section:\n%s", out)
	}
	if !strings.Contains(out, "### From `CONTRIBUTING.md`") {
		t.Errorf("missing CONTRIBUTING section:\n%s", out)
	}
	if !strings.Contains(out, "Use tabs.") || !strings.Contains(out, "Prefer stdlib.") {
		t.Errorf("body missing:\n%s", out)
	}
	// AGENTS must precede CONTRIBUTING (priority order).
	if strings.Index(out, "AGENTS.md") > strings.Index(out, "CONTRIBUTING.md") {
		t.Errorf("priority order broken:\n%s", out)
	}
}

func TestLoad_CursorRules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".cursor/rules/01-style.md"), "prefer early returns\n")
	writeFile(t, filepath.Join(dir, ".cursor/rules/02-naming.md"), "camelCase in Go\n")
	// Non-md files must be ignored.
	writeFile(t, filepath.Join(dir, ".cursor/rules/other.txt"), "ignored\n")

	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, ".cursor/rules/01-style.md") {
		t.Errorf("missing 01-style section:\n%s", out)
	}
	if !strings.Contains(out, ".cursor/rules/02-naming.md") {
		t.Errorf("missing 02-naming section:\n%s", out)
	}
	if strings.Contains(out, "other.txt") {
		t.Errorf("non-md leaked in:\n%s", out)
	}
	// Sorted order: 01 before 02.
	if strings.Index(out, "01-style") > strings.Index(out, "02-naming") {
		t.Errorf("cursor rules not sorted:\n%s", out)
	}
}

func TestLoad_StripsBoilerplate(t *testing.T) {
	body := strings.Join([]string{
		"# Coding Rules",
		"Use gofmt.",
		"",
		"## Installation",
		"Run `make`.",
		"Skip me.",
		"",
		"## Style",
		"Prefer early returns.",
	}, "\n")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), body)

	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Contains(out, "Installation") {
		t.Errorf("Installation header not stripped:\n%s", out)
	}
	if strings.Contains(out, "Run `make`") || strings.Contains(out, "Skip me.") {
		t.Errorf("boilerplate body leaked:\n%s", out)
	}
	if !strings.Contains(out, "Use gofmt.") {
		t.Errorf("real content dropped:\n%s", out)
	}
	if !strings.Contains(out, "Prefer early returns.") {
		t.Errorf("post-boilerplate content dropped:\n%s", out)
	}
}

func TestLoad_TotalCapEnforced(t *testing.T) {
	dir := t.TempDir()
	// Two per-file-cap-hitting files plus a marker file that must be dropped.
	// 4000 (AGENTS body) + 4000 (CONTRIBUTING body) + headers + joiners > 8000,
	// so CLAUDE.md never makes it in.
	writeFile(t, filepath.Join(dir, "AGENTS.md"), strings.Repeat("a", maxPerFileChars*2))
	writeFile(t, filepath.Join(dir, "CONTRIBUTING.md"), strings.Repeat("b", maxPerFileChars*2))
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "MARKER_SHOULD_BE_DROPPED\n")

	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out) > MaxTotalChars {
		t.Errorf("output exceeds cap: len=%d cap=%d", len(out), MaxTotalChars)
	}
	if strings.Contains(out, "MARKER_SHOULD_BE_DROPPED") {
		t.Errorf("third file leaked past cap: len=%d", len(out))
	}
	if !strings.Contains(out, "…(truncated)") {
		t.Errorf("expected per-file truncation marker")
	}
}

func TestLoad_MissingCursorDirNotError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "ok\n")
	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("content missing:\n%s", out)
	}
}

func TestLoad_EmptyFileSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "")
	writeFile(t, filepath.Join(dir, "CONTRIBUTING.md"), "real content\n")
	out, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Contains(out, "AGENTS.md") {
		t.Errorf("empty file created a section:\n%s", out)
	}
	if !strings.Contains(out, "real content") {
		t.Errorf("real content missing:\n%s", out)
	}
}

func TestStripBoilerplate_HigherLevelReopens(t *testing.T) {
	// Higher-level header (## after ###) must end the skip block.
	in := strings.Join([]string{
		"# Top",
		"kept",
		"### Installation",
		"drop",
		"## Naming",
		"also kept",
	}, "\n")
	got := stripBoilerplate(in)
	if !strings.Contains(got, "kept") || !strings.Contains(got, "also kept") {
		t.Errorf("skip block ended too late:\n%s", got)
	}
	if strings.Contains(got, "drop") {
		t.Errorf("boilerplate body leaked:\n%s", got)
	}
}

func TestStripBoilerplate_EmptyInput(t *testing.T) {
	if got := stripBoilerplate(""); got != "" {
		t.Fatalf("expected empty output, got %q", got)
	}
}
