// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// writeSampleRulesRepo lays out a minimal ORG_RULES_REPO layout on disk: one
// rule under rules/. Returns the directory path.
func writeSampleRulesRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `id: no-println
title: No stray println
body: Do not check in println statements
scope: global
severity: warning
`
	if err := os.WriteFile(filepath.Join(dir, "rules", "no-println.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestLoadRulesFromLocalDir(t *testing.T) {
	dir := writeSampleRulesRepo(t)
	t.Setenv("SACR_ORG_RULES_REPO", dir) // absolute path → local-dir path
	block, err := loadRules(context.Background(), []model.Diff{{NewPath: "a.go"}})
	if err != nil {
		t.Fatalf("loadRules: %v", err)
	}
	if !strings.Contains(block, "No stray println") {
		t.Errorf("expected title in prompt block: %q", block)
	}
}

func TestLoadRulesNoSource(t *testing.T) {
	t.Setenv("SACR_ORG_RULES_REPO", "")
	block, err := loadRules(context.Background(), []model.Diff{{NewPath: "a.go"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if block != "" {
		t.Errorf("expected empty block, got %q", block)
	}
}

func TestRunRulesListWithLocalDir(t *testing.T) {
	dir := writeSampleRulesRepo(t)
	cmd := newRulesListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--source", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "no-println") {
		t.Errorf("expected rule id in output: %s", got)
	}
	if !strings.Contains(got, "global") {
		t.Errorf("expected scope in output: %s", got)
	}
}

func TestRunRulesListWithoutSourceErrors(t *testing.T) {
	t.Setenv("SACR_ORG_RULES_REPO", "")
	cmd := newRulesListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error without source")
	}
	if !strings.Contains(err.Error(), "no rules source") {
		t.Errorf("wrong err: %v", err)
	}
}

func TestRunRulesSyncWithoutSourceErrors(t *testing.T) {
	t.Setenv("SACR_ORG_RULES_REPO", "")
	cmd := newRulesSyncCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error without source")
	}
}

func TestBuildToolRegistryRegistersAllExpected(t *testing.T) {
	kept := []model.Diff{
		{NewPath: "a.go", Diff: "diff a"},
	}
	opts := &reviewOpts{Repo: ".", Format: "stdout"}
	reg, lookup, err := buildToolRegistry(".", kept, opts)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, name := range []string{"file_read", "file_find", "code_search", "file_read_diff"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("registry missing %s", name)
		}
	}
	if d := lookup("a.go"); d == nil || d.Diff != "diff a" {
		t.Errorf("lookup: %+v", d)
	}
	if d := lookup("missing.go"); d != nil {
		t.Errorf("lookup for missing path should return nil, got %+v", d)
	}
}

func TestMaybeDetectOverlapDisabledPaths(t *testing.T) {
	// No GITHUB_TOKEN → nil.
	t.Setenv("GITHUB_TOKEN", "")
	got := maybeDetectOverlap(context.Background(), &reviewOpts{PR: 1}, nil, nil)
	if got != nil {
		t.Errorf("no token: expected nil, got %+v", got)
	}
	// Explicit disable.
	t.Setenv("GITHUB_TOKEN", "abc")
	t.Setenv("SACR_OVERLAP_ENABLED", "0")
	got = maybeDetectOverlap(context.Background(), &reviewOpts{PR: 1}, nil, nil)
	if got != nil {
		t.Errorf("disabled: expected nil, got %+v", got)
	}
	// No PR → nil.
	t.Setenv("SACR_OVERLAP_ENABLED", "")
	got = maybeDetectOverlap(context.Background(), &reviewOpts{PR: 0}, nil, nil)
	if got != nil {
		t.Errorf("no PR: expected nil, got %+v", got)
	}
	// No GITHUB_REPOSITORY → nil.
	t.Setenv("GITHUB_REPOSITORY", "")
	got = maybeDetectOverlap(context.Background(), &reviewOpts{PR: 1}, nil, nil)
	if got != nil {
		t.Errorf("no repo env: expected nil, got %+v", got)
	}
}

func TestIndexCmdRequiresDSN(t *testing.T) {
	t.Setenv("SACR_DB_URL", "")
	cmd := newIndexCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--repo", "."})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected err without SACR_DB_URL")
	}
	if !strings.Contains(err.Error(), "SACR_DB_URL") {
		t.Errorf("wrong err: %v", err)
	}
}

func TestOverlapCmdRequiresFlags(t *testing.T) {
	cmd := newOverlapCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{}) // no flags
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected err without required flags")
	}
}

func TestNewLLMTiersReturnsErrWithoutConfig(t *testing.T) {
	for _, k := range []string{
		"OCR_LLM_URL", "OCR_LLM_TOKEN", "OCR_LLM_MODEL",
		"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL",
		"SACR_PROVIDER", "SACR_MODEL",
		"SACR_CHEAP_PROVIDER", "SACR_CHEAP_MODEL",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("HOME", t.TempDir())
	_, err := newLLMTiers()
	if err == nil {
		t.Fatal("expected err with no LLM env configured")
	}
}
