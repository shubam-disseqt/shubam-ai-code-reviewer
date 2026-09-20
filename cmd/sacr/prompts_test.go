// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"strings"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

func TestLoadPromptExists(t *testing.T) {
	for _, name := range []string{
		"main_task_system.md",
		"main_task_user.md",
		"memory_compression_task_system.md",
		"memory_compression_task_user.md",
	} {
		s, err := loadPrompt(name)
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		if len(s) == 0 {
			t.Errorf("%s: empty body", name)
		}
	}
}

func TestLoadPromptMissing(t *testing.T) {
	if _, err := loadPrompt("does_not_exist.md"); err == nil {
		t.Fatal("expected error for missing template")
	}
}

func TestRenderUserPromptSubstitutes(t *testing.T) {
	tmpl := "hi {{name}}, at {{time}}"
	out := renderUserPrompt(tmpl, map[string]string{"name": "world", "time": "now"})
	if out != "hi world, at now" {
		t.Errorf("got %q", out)
	}
}

func TestBuildReviewMessagesShape(t *testing.T) {
	msgs := buildReviewMessages("SYS", "user {{diffs}} end", "RULES", "CTX", "", "[\"a.go\"]", "DIFF")
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Errorf("wrong roles: %+v", msgs)
	}
	sys := msgs[0].ExtractText()
	if !strings.Contains(sys, "SYS") || !strings.Contains(sys, "Codebase Context") || !strings.Contains(sys, "CTX") {
		t.Errorf("system missing content: %q", sys)
	}
	user := msgs[1].ExtractText()
	if !strings.Contains(user, "user DIFF end") {
		t.Errorf("user did not substitute {{diffs}}: %q", user)
	}
}

func TestBuildReviewMessagesEmptyContext(t *testing.T) {
	msgs := buildReviewMessages("SYS", "u", "", "", "", "", "")
	sys := msgs[0].ExtractText()
	if strings.Contains(sys, "Codebase Context") {
		t.Errorf("empty context should skip section header: %q", sys)
	}
	if strings.Contains(sys, "Known Issues") {
		t.Errorf("empty known issues should skip section header: %q", sys)
	}
}

func TestBuildReviewMessagesInjectsKnownIssues(t *testing.T) {
	known := "## Known Issues (from static analysis)\n\n- [gitleaks / secret / HIGH] a.go:1 — foo\n"
	msgs := buildReviewMessages("SYS", "u", "", "", known, "", "")
	sys := msgs[0].ExtractText()
	if !strings.Contains(sys, "Known Issues") {
		t.Errorf("known-issues block missing: %q", sys)
	}
	if !strings.Contains(sys, "gitleaks") {
		t.Errorf("known-issues body missing: %q", sys)
	}
}

func TestRenderDiffsForFileWrapsPath(t *testing.T) {
	d := model.Diff{NewPath: "foo/bar.go", Diff: "diff body"}
	got := renderDiffsForFile(d)
	if !strings.Contains(got, `<file path="foo/bar.go">`) {
		t.Errorf("missing file tag: %s", got)
	}
	if !strings.Contains(got, "diff body") {
		t.Errorf("missing body: %s", got)
	}
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), "</file>") {
		t.Errorf("missing close tag: %s", got)
	}
}

func TestRenderDiffsForFileDeletedUsesOldPath(t *testing.T) {
	d := model.Diff{OldPath: "old.go", NewPath: "/dev/null", Diff: "x\n"}
	got := renderDiffsForFile(d)
	if !strings.Contains(got, `<file path="old.go">`) {
		t.Errorf("deleted diff should render old path: %s", got)
	}
}

// TestPromptUser_ShowsRenameHint verifies that renamed files carry a
// renamed_from attribute so the reviewer can distinguish a move from a
// delete+add pair.
func TestPromptUser_ShowsRenameHint(t *testing.T) {
	d := model.Diff{
		OldPath:    "internal/data/store.go",
		NewPath:    "internal/store/store.go",
		Diff:       "diff body with one changed line\n",
		IsRenamed:  true,
		Insertions: 1,
		Deletions:  1,
	}
	got := renderDiffsForFile(d)
	if !strings.Contains(got, `renamed_from="internal/data/store.go"`) {
		t.Errorf("missing renamed_from attribute: %s", got)
	}
	if !strings.Contains(got, `<file path="internal/store/store.go"`) {
		t.Errorf("missing new path attribute: %s", got)
	}
	if !strings.Contains(got, "diff body with one changed line") {
		t.Errorf("body dropped for non-pure rename: %s", got)
	}
}

// TestPromptUser_PureRenameCollapsesBody verifies that a 100%-similarity
// rename (no line changes) renders a one-line note instead of an empty diff.
func TestPromptUser_PureRenameCollapsesBody(t *testing.T) {
	d := model.Diff{
		OldPath:   "pkg/old.go",
		NewPath:   "pkg/new.go",
		Diff:      "diff --git a/pkg/old.go b/pkg/new.go\nsimilarity index 100%\nrename from pkg/old.go\nrename to pkg/new.go\n",
		IsRenamed: true,
	}
	got := renderDiffsForFile(d)
	if !strings.Contains(got, `renamed_from="pkg/old.go"`) {
		t.Errorf("missing renamed_from: %s", got)
	}
	if !strings.Contains(got, "with no content change") {
		t.Errorf("pure rename should render note, got: %s", got)
	}
	if strings.Contains(got, "similarity index 100%") {
		t.Errorf("pure rename should not echo raw diff header: %s", got)
	}
}

func TestIsPureRename(t *testing.T) {
	tests := []struct {
		name string
		d    model.Diff
		want bool
	}{
		{"pure rename", model.Diff{IsRenamed: true}, true},
		{"rename with edits", model.Diff{IsRenamed: true, Insertions: 3}, false},
		{"rename with deletions", model.Diff{IsRenamed: true, Deletions: 1}, false},
		{"not a rename", model.Diff{IsNew: true}, false},
	}
	for _, tt := range tests {
		if got := isPureRename(tt.d); got != tt.want {
			t.Errorf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}

func TestRenderChangedFilesJSON(t *testing.T) {
	diffs := []model.Diff{
		{NewPath: "a.go"},
		{OldPath: "b.go", NewPath: "/dev/null"},
		{NewPath: "c.go"},
	}
	got := renderChangedFilesJSON(diffs)
	if got != `["a.go","b.go","c.go"]` {
		t.Errorf("got %q", got)
	}
}

func TestLoadMainToolDefs(t *testing.T) {
	defs, err := loadMainToolDefs()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("expected some tool defs")
	}
	// Sanity: task_done and code_comment must be present.
	seen := map[string]bool{}
	for _, d := range defs {
		seen[d.Function.Name] = true
	}
	for _, want := range []string{"task_done", "code_comment"} {
		if !seen[want] {
			t.Errorf("main tool defs missing %s", want)
		}
	}
}

func TestBuildCompressionTemplate(t *testing.T) {
	tpl, err := buildCompressionTemplate()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(tpl.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(tpl.Messages))
	}
	if tpl.Messages[0].Role != "system" || tpl.Messages[1].Role != "user" {
		t.Errorf("wrong roles: %+v", tpl.Messages)
	}
}
