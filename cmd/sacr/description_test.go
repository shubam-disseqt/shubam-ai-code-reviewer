// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/effort"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/scoring"
)

// fakePRClient records calls and serves canned responses. Kept in the test
// file so nothing in production code depends on it.
type fakePRClient struct {
	body       string
	getErr     error
	updateErr  error
	labelsErr  error
	updated    string
	updates    int
	lastLabels []string
	labelCalls int
}

func (f *fakePRClient) GetPRBody(ctx context.Context, owner, repo string, number int) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.body, nil
}

func (f *fakePRClient) UpdatePRBody(ctx context.Context, owner, repo string, number int, body string) error {
	f.updates++
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updated = body
	return nil
}

func (f *fakePRClient) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	f.labelCalls++
	if f.labelsErr != nil {
		return f.labelsErr
	}
	f.lastLabels = append([]string(nil), labels...)
	return nil
}

func (f *fakePRClient) ListLabels(ctx context.Context, owner, repo string, number int) ([]string, error) {
	return nil, nil
}

func (f *fakePRClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	return nil
}

func TestUpdateDescription_AppendsBlockWhenMissing(t *testing.T) {
	f := &fakePRClient{body: "Original PR description here.\n\nCloses #1."}
	err := UpdateDescription(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "adds a widget"},
		model.Labels{},
		map[scoring.Severity]int{scoring.SeverityHigh: 1}, nil, nil, effort.Score{}, "")
	if err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}
	if !strings.Contains(f.updated, "Original PR description here.") {
		t.Errorf("original preserved? updated=\n%s", f.updated)
	}
	if !strings.Contains(f.updated, sacrBlockBegin) || !strings.Contains(f.updated, sacrBlockEnd) {
		t.Errorf("markers missing:\n%s", f.updated)
	}
	if !strings.Contains(f.updated, "adds a widget") {
		t.Errorf("walkthrough missing:\n%s", f.updated)
	}
	if !strings.Contains(f.updated, "| HIGH | 1 |") {
		t.Errorf("severity row missing:\n%s", f.updated)
	}
}

func TestUpdateDescription_ReplacesExistingBlock(t *testing.T) {
	// Two runs must not cause the block to accumulate.
	initial := "Keep me.\n\n" + sacrBlockBegin + "\nOLD CONTENT\n" + sacrBlockEnd + "\n\nAnd keep me too."
	f := &fakePRClient{body: initial}
	err := UpdateDescription(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "fresh walkthrough"},
		model.Labels{},
		map[scoring.Severity]int{}, nil, nil, effort.Score{}, "")
	if err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}
	if strings.Contains(f.updated, "OLD CONTENT") {
		t.Errorf("old block still present:\n%s", f.updated)
	}
	if strings.Count(f.updated, sacrBlockBegin) != 1 || strings.Count(f.updated, sacrBlockEnd) != 1 {
		t.Errorf("markers not idempotent:\n%s", f.updated)
	}
	if !strings.Contains(f.updated, "Keep me.") || !strings.Contains(f.updated, "And keep me too.") {
		t.Errorf("surrounding content lost:\n%s", f.updated)
	}
	if !strings.Contains(f.updated, "fresh walkthrough") {
		t.Errorf("new content missing:\n%s", f.updated)
	}
}

func TestUpdateDescription_NoChangeSkipsUpdate(t *testing.T) {
	// If the block already contains exactly what we would write, no PATCH
	// should fire — saves an API call per idempotent re-run.
	block := renderSacrBlock(model.Summary{Walkthrough: "same"}, model.Labels{}, map[scoring.Severity]int{}, nil, nil, effort.Score{}, "")
	initial := sacrBlockBegin + "\n" + block + "\n" + sacrBlockEnd
	f := &fakePRClient{body: initial}
	err := UpdateDescription(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "same"},
		model.Labels{},
		map[scoring.Severity]int{}, nil, nil, effort.Score{}, "")
	if err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}
	if f.updates != 0 {
		t.Errorf("UpdatePRBody called %d times, want 0", f.updates)
	}
}

func TestUpdateDescription_AppliesLabels(t *testing.T) {
	f := &fakePRClient{body: ""}
	err := UpdateDescription(context.Background(), f, "o", "r", 7,
		model.Summary{},
		model.Labels{PRType: "feat", RiskTag: "high", Domains: []string{"auth", "billing"}},
		map[scoring.Severity]int{}, nil, nil, effort.Score{}, "")
	if err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}
	if f.labelCalls != 1 {
		t.Fatalf("AddLabels calls = %d, want 1", f.labelCalls)
	}
	got := strings.Join(f.lastLabels, ",")
	for _, want := range []string{"feat", "high", "auth", "billing"} {
		if !strings.Contains(got, want) {
			t.Errorf("labels missing %q: %v", want, f.lastLabels)
		}
	}
}

func TestUpdateDescription_EmptyBodyGetsBlock(t *testing.T) {
	f := &fakePRClient{body: ""}
	err := UpdateDescription(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "hello"},
		model.Labels{},
		map[scoring.Severity]int{}, nil, nil, effort.Score{}, "")
	if err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}
	if !strings.HasPrefix(f.updated, sacrBlockBegin) {
		t.Errorf("empty body should start with marker:\n%s", f.updated)
	}
}

func TestUpdateDescription_GetErrorPropagates(t *testing.T) {
	f := &fakePRClient{getErr: errors.New("boom")}
	err := UpdateDescription(context.Background(), f, "o", "r", 7,
		model.Summary{}, model.Labels{}, nil, nil, nil, effort.Score{}, "")
	if err == nil {
		t.Fatalf("want error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want wrapped", err)
	}
}

func TestUpdateDescription_UpdateErrorPropagates(t *testing.T) {
	f := &fakePRClient{updateErr: errors.New("nope")}
	err := UpdateDescription(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "hi"}, model.Labels{}, nil, nil, nil, effort.Score{}, "")
	if err == nil {
		t.Fatalf("want error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %v, want wrapped", err)
	}
}

func TestUpdateDescription_MissingIdentity(t *testing.T) {
	f := &fakePRClient{}
	err := UpdateDescription(context.Background(), f, "", "r", 7, model.Summary{}, model.Labels{}, nil, nil, nil, effort.Score{}, "")
	if err == nil {
		t.Errorf("empty owner should error")
	}
	err = UpdateDescription(context.Background(), f, "o", "r", 0, model.Summary{}, model.Labels{}, nil, nil, nil, effort.Score{}, "")
	if err == nil {
		t.Errorf("zero pr should error")
	}
	err = UpdateDescription(context.Background(), nil, "o", "r", 7, model.Summary{}, model.Labels{}, nil, nil, nil, effort.Score{}, "")
	if err == nil {
		t.Errorf("nil client should error")
	}
}

func TestRenderSacrBlock_ChangeGroups(t *testing.T) {
	block := renderSacrBlock(
		model.Summary{
			Walkthrough:  "does two things",
			ChangeGroups: []model.ChangeGroup{{Title: "Auth", Files: []string{"a.go"}, Summary: "rework"}},
			TestingNotes: "run go test",
		},
		model.Labels{RiskTag: "high"},
		map[scoring.Severity]int{scoring.SeverityCritical: 2, scoring.SeverityLow: 1},
		nil,
		nil,
		effort.Score{}, "")
	for _, want := range []string{
		"does two things",
		"| Severity | Count |",
		"| CRITICAL | 2 |",
		"| LOW | 1 |",
		"**Risk:** `high`",
		"### Change groups",
		"**Auth**",
		"a.go",
		"### Testing notes",
		"run go test",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q\n---\n%s", want, block)
		}
	}
}

func TestScoreCountsFromMap(t *testing.T) {
	scores := map[string]scoring.Score{
		"a": {Severity: scoring.SeverityCritical},
		"b": {Severity: scoring.SeverityHigh},
		"c": {Severity: scoring.SeverityHigh},
		"d": {Severity: scoring.SeverityLow},
	}
	got := scoreCountsFromMap(scores)
	if got[scoring.SeverityCritical] != 1 || got[scoring.SeverityHigh] != 2 || got[scoring.SeverityLow] != 1 {
		t.Errorf("counts = %v", got)
	}
	if _, ok := got[scoring.SeverityMedium]; !ok {
		t.Errorf("medium row missing — every severity must appear even when zero")
	}
}

func TestLabelSet_Dedup(t *testing.T) {
	got := labelSet(model.Labels{
		PRType:         "feat",
		RiskTag:        "feat", // dup on purpose
		Domains:        []string{"auth", "", "auth"},
		OwnershipHints: []string{"team-a"},
	})
	if len(got) != 3 {
		t.Errorf("dedup failed: %v", got)
	}
}

// TestStaleExclusiveLabels_DropsStalePRType guards against a reviewer's
// pr_type flipping between runs (e.g. "fix" → "feat" after the labeler
// prompt got tightened) leaving both labels applied. Regression: only
// risk/* used to be treated as exclusive, so pr_type labels accumulated.
func TestStaleExclusiveLabels_DropsStalePRType(t *testing.T) {
	existing := []string{"backend", "fix", "risk/critical", "pricing"}
	fresh := []string{"backend", "feat", "risk/high", "pricing"}

	drop := staleExclusiveLabels(existing, fresh)
	dropSet := make(map[string]bool, len(drop))
	for _, d := range drop {
		dropSet[d] = true
	}

	if !dropSet["fix"] {
		t.Errorf("expected 'fix' to be dropped (stale pr_type replaced by 'feat'); got: %v", drop)
	}
	if !dropSet["risk/critical"] {
		t.Errorf("expected 'risk/critical' to be dropped (stale risk replaced by 'risk/high'); got: %v", drop)
	}
	if dropSet["backend"] || dropSet["pricing"] {
		t.Errorf("non-exclusive labels should not be dropped: %v", drop)
	}
}

// TestStaleExclusiveLabels_LeavesPRTypeAloneWhenLabelerSilent verifies we
// don't clobber a human's manually-applied pr_type label when the labeler
// returns no pr_type. This protects mixed human+bot label workflows.
func TestStaleExclusiveLabels_LeavesPRTypeAloneWhenLabelerSilent(t *testing.T) {
	existing := []string{"test", "risk/medium"}
	// Fresh set has no pr_type value (labeler failed / returned empty).
	fresh := []string{"risk/high"}

	drop := staleExclusiveLabels(existing, fresh)
	for _, d := range drop {
		if d == "test" {
			t.Errorf("must not drop 'test' when fresh set has no pr_type; got drops: %v", drop)
		}
	}
}
