// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/scoring"
)

func TestReviewOptsValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    reviewOpts
		wantErr string
	}{
		{
			name:    "commit + from is rejected",
			opts:    reviewOpts{Commit: "abc", From: "main", Format: "stdout"},
			wantErr: "mutually exclusive",
		},
		{
			name:    "commit + to is rejected",
			opts:    reviewOpts{Commit: "abc", To: "HEAD", Format: "stdout"},
			wantErr: "mutually exclusive",
		},
		{
			name:    "unknown format is rejected",
			opts:    reviewOpts{Format: "yaml"},
			wantErr: "--format must be",
		},
		{
			name:    "github format without pr is rejected",
			opts:    reviewOpts{Format: "github"},
			wantErr: "--pr",
		},
		{
			name:    "workspace mode is fine",
			opts:    reviewOpts{Format: "stdout", MinSeverity: "MEDIUM"},
			wantErr: "",
		},
		{
			name:    "commit-only is fine",
			opts:    reviewOpts{Commit: "abc", Format: "json", MinSeverity: "MEDIUM"},
			wantErr: "",
		},
		{
			name:    "min-severity invalid is rejected",
			opts:    reviewOpts{Format: "stdout", MinSeverity: "purple"},
			wantErr: "--min-severity",
		},
		{
			name:    "min-severity SUPPRESS is rejected",
			opts:    reviewOpts{Format: "stdout", MinSeverity: "SUPPRESS"},
			wantErr: "SUPPRESS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected err containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestReviewCmdHelpMentionsFlags(t *testing.T) {
	cmd := newReviewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	got := buf.String()
	for _, flag := range []string{"--from", "--to", "--commit", "--repo", "--format", "--output", "--pr", "--resume", "--verbose", "--min-severity"} {
		if !strings.Contains(got, flag) {
			t.Errorf("--help output missing flag %s", flag)
		}
	}
}

func TestReviewCmdRejectsUnknownFormat(t *testing.T) {
	root := newRootCmd()
	var stderr bytes.Buffer
	root.SetErr(&stderr)
	root.SetOut(&stderr)
	root.SetArgs([]string{"review", "--format", "yaml"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected err for unknown --format, got nil")
	}
	if !strings.Contains(err.Error(), "--format must be") {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestComputeReviewerEffort_NewFilePropagates guards the effort audit
// table from silently dropping the "new files" contribution. Regression:
// the translation used to re-derive IsNew from empty OldPath, but the diff
// parser sets OldPath="a/foo.go" on new files and IsNew=true separately.
func TestComputeReviewerEffort_NewFilePropagates(t *testing.T) {
	diffs := []model.Diff{
		{
			OldPath: "pricing.go",
			NewPath: "pricing.go",
			IsNew:   true,
			Diff:    "+package main\n+\n+func F() {}\n",
		},
	}
	got := computeReviewerEffort("/tmp/nonexistent", diffs, map[string]scoring.Score{}, 0, slog.Default())
	found := false
	for _, c := range got.Contributions {
		if c.Signal == "New files" {
			found = true
			if c.Points <= 0 {
				t.Errorf("New files contribution present but zero: %+v", c)
			}
		}
	}
	if !found {
		t.Errorf("New files contribution missing; got %d rows: %+v", len(got.Contributions), got.Contributions)
	}
}

// TestLabelForNonReviewablePaths guards the classification used by the
// no-reviewable-changes block so a docs-only PR gets `docs` and a
// go.mod-only PR gets `chore` (not silent no-op).
func TestLabelForNonReviewablePaths(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  string
	}{
		{"docs only md", []string{"docs/architecture.md", "README.md"}, "docs"},
		{"go.mod + go.sum", []string{"go.mod", "go.sum"}, "chore"},
		{"package.json", []string{"package.json", "package-lock.json"}, "chore"},
		{"mixed docs and deps", []string{"README.md", "go.mod"}, "chore"},
		{"empty", []string{}, "chore"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diffs := make([]model.Diff, len(tc.paths))
			for i, p := range tc.paths {
				diffs[i] = model.Diff{NewPath: p}
			}
			got := labelForNonReviewablePaths(diffs)
			if got != tc.want {
				t.Errorf("labelForNonReviewablePaths(%v) = %q, want %q", tc.paths, got, tc.want)
			}
		})
	}
}

func TestReviewCmdRejectsCommitPlusRange(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"review", "--commit", "abc", "--from", "main"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected err for --commit + --from, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("wrong error: %v", err)
	}
}
