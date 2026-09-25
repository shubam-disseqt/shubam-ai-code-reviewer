// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/findings"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

func TestRunCarryoverSkipsWithoutPR(t *testing.T) {
	comments := []model.LlmComment{{Path: "a.go", StartLine: 1, EndLine: 1, Content: "x"}}
	var buf bytes.Buffer
	got := runCarryover(comments, []string{"a.go"}, "o", "r", 0, nil, testLogger(&buf))
	if len(got.Comments) != 1 {
		t.Errorf("want 1 comment untouched, got %d", len(got.Comments))
	}
	if got.State != nil {
		t.Errorf("expected nil state map when skipped, got %v", got.State)
	}
}

func TestRunCarryoverSkipsWithoutOwnerRepo(t *testing.T) {
	comments := []model.LlmComment{{Path: "a.go", StartLine: 1, EndLine: 1, Content: "x"}}
	var buf bytes.Buffer
	got := runCarryover(comments, []string{"a.go"}, "", "", 42, nil, testLogger(&buf))
	if len(got.Comments) != 1 {
		t.Errorf("want 1 comment untouched, got %d", len(got.Comments))
	}
}

func TestRunCarryoverPersistsAndReturnsState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SACR_FINDINGS_DIR", dir)

	comments := []model.LlmComment{
		{Path: "a.go", StartLine: 10, EndLine: 12, Content: "x", Category: "bug", ExistingCode: "if err != nil { return err }"},
	}
	var buf bytes.Buffer
	got := runCarryover(comments, []string{"a.go"}, "o", "r", 42, nil, testLogger(&buf))

	if len(got.Comments) != 1 {
		t.Fatalf("want 1 comment, got %d", len(got.Comments))
	}
	if len(got.State) != 1 {
		t.Fatalf("want 1 state entry, got %d", len(got.State))
	}
	// First pass ⇒ state must be "new".
	for _, st := range got.State {
		if st != findings.StateNew {
			t.Errorf("expected new on first pass, got %s", st)
		}
	}
	// File is persisted.
	if _, err := os.Stat(filepath.Join(dir, "o_r_42.json")); err != nil {
		t.Errorf("expected persisted findings file, got %v", err)
	}
	// Log line reflects the counts.
	if !strings.Contains(buf.String(), "1 new") {
		t.Errorf("log missing 'new' count: %s", buf.String())
	}
}

func TestRunCarryoverReconcilesAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SACR_FINDINGS_DIR", dir)

	// First pass: two comments.
	first := []model.LlmComment{
		{Path: "a.go", StartLine: 10, EndLine: 12, Content: "x", Category: "bug", ExistingCode: "if err != nil { return err }"},
		{Path: "b.go", StartLine: 5, EndLine: 5, Content: "y", Category: "style", ExistingCode: "return  nil"},
	}
	var buf1 bytes.Buffer
	_ = runCarryover(first, []string{"a.go", "b.go"}, "o", "r", 42, nil, testLogger(&buf1))

	// Second pass: only re-finds a.go, but the diff touches ONLY a.go so
	// b.go's finding should carry (file untouched).
	second := []model.LlmComment{
		{Path: "a.go", StartLine: 10, EndLine: 12, Content: "x", Category: "bug", ExistingCode: "if err != nil { return err }"},
	}
	var buf2 bytes.Buffer
	got := runCarryover(second, []string{"a.go"}, "o", "r", 42, nil, testLogger(&buf2))

	if len(got.Comments) != 2 {
		t.Fatalf("want 2 (keep + carried), got %d: %+v", len(got.Comments), got.Comments)
	}
	states := map[findings.State]int{}
	for _, st := range got.State {
		states[st]++
	}
	if states[findings.StateKeep] != 1 || states[findings.StateCarried] != 1 {
		t.Errorf("state distribution wrong: %v", states)
	}
	if !strings.Contains(buf2.String(), "1 carried") {
		t.Errorf("log missing 'carried': %s", buf2.String())
	}
}

func TestRunCarryoverDropsResolved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SACR_FINDINGS_DIR", dir)

	// First pass records the finding.
	first := []model.LlmComment{
		{Path: "a.go", StartLine: 10, EndLine: 12, Content: "x", Category: "bug", ExistingCode: "if err != nil { return err }"},
	}
	_ = runCarryover(first, []string{"a.go"}, "o", "r", 42, nil, testLogger(&bytes.Buffer{}))

	// Second pass: file is touched again but no matching finding → resolved.
	var buf bytes.Buffer
	got := runCarryover(nil, []string{"a.go"}, "o", "r", 42, nil, testLogger(&buf))
	if len(got.Comments) != 0 {
		t.Errorf("expected empty (resolved dropped), got %d", len(got.Comments))
	}
	if !strings.Contains(buf.String(), "1 resolved") {
		t.Errorf("log missing 'resolved': %s", buf.String())
	}
}

func TestEmittedCommentIncludesState(t *testing.T) {
	// A JSON emit for a carried finding must include the state field so
	// a downstream poster can update rather than duplicate.
	c := model.LlmComment{
		Path: "a.go", StartLine: 1, EndLine: 1, Content: "x", Category: "bug",
	}
	fp := commentFingerprint("o", "r", c)
	var buf bytes.Buffer
	err := emit(nil, emitConfig{
		Format:       formatJSON,
		Output:       "-",
		Comments:     []model.LlmComment{c},
		Stdout:       &buf,
		Owner:        "o",
		Repo:         "r",
		FindingState: map[string]findings.State{fp: findings.StateCarried},
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	var out struct {
		Comments []struct {
			State string `json:"state"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v (out=%s)", err, buf.String())
	}
	if len(out.Comments) != 1 || out.Comments[0].State != "carried" {
		t.Errorf("state not emitted: %s", buf.String())
	}
}

// CI runners have no ~/.sacr/findings; the previous state must come from
// the fingerprint markers already on the PR.
func TestRunCarryoverRecoversStateFromPRComments(t *testing.T) {
	t.Setenv("SACR_FINDINGS_DIR", t.TempDir())
	fresh := model.LlmComment{Path: "a.go", StartLine: 10, EndLine: 12, Content: "x", Category: "bug", ExistingCode: "if err != nil { return err }"}
	existing := []gh.ExistingComment{
		{ID: 1, Path: "a.go", Line: 10, Body: "**[high / bug]** x\n\n<!-- sacr:fp:" + commentFingerprint("o", "r", fresh) + " -->"},
		{ID: 2, Path: "a.go", Line: 40, Body: "**[low / style]** gone\n\n<!-- sacr:fp:deadbeef -->"},
		{ID: 3, Path: "a.go", Line: 5, Body: "human comment, no marker"},
	}
	var buf bytes.Buffer

	got := runCarryover([]model.LlmComment{fresh}, []string{"a.go"}, "o", "r", 42, existing, testLogger(&buf))

	if st := got.State[commentFingerprint("o", "r", fresh)]; st != findings.StateKeep {
		t.Errorf("matching marker must be kept, got %q", st)
	}
	if got.Counts.Kept != 1 || got.Counts.Resolved != 1 || got.Counts.New != 0 {
		t.Errorf("counts: %+v", got.Counts)
	}
	for _, want := range []string{"recovered 2 finding(s)", "1 kept", "1 resolved", "0 new"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log missing %q:\n%s", want, buf.String())
		}
	}
}
