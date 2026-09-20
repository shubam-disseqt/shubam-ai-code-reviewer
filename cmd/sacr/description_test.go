// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/effort"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/scoring"
)

// fakeSummaryClient records issue-comment API calls.
type fakeSummaryClient struct {
	existing   []gh.IssueComment
	listErr    error
	postErr    error
	deleteErr  error
	posted     []string
	deleted    []int64
	postCalls  int
	nextID     int64
	deleteCall int
}

func (f *fakeSummaryClient) PostIssueComment(ctx context.Context, owner, repo string, number int, body string) (gh.IssueComment, error) {
	f.postCalls++
	if f.postErr != nil {
		return gh.IssueComment{}, f.postErr
	}
	f.posted = append(f.posted, body)
	f.nextID++
	ic := gh.IssueComment{ID: 1000 + f.nextID, Body: body}
	f.existing = append(f.existing, ic)
	return ic, nil
}

func (f *fakeSummaryClient) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]gh.IssueComment, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]gh.IssueComment(nil), f.existing...), nil
}

func (f *fakeSummaryClient) DeleteIssueComment(ctx context.Context, owner, repo string, commentID int64) error {
	f.deleteCall++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, commentID)
	kept := f.existing[:0]
	for _, c := range f.existing {
		if c.ID != commentID {
			kept = append(kept, c)
		}
	}
	f.existing = kept
	return nil
}

// fakeLabelClient records label API calls.
type fakeLabelClient struct {
	existing   []string
	listErr    error
	addErr     error
	removeErr  error
	addCalls   int
	lastLabels []string
	removed    []string
}

func (f *fakeLabelClient) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	f.addCalls++
	if f.addErr != nil {
		return f.addErr
	}
	f.lastLabels = append([]string(nil), labels...)
	return nil
}

func (f *fakeLabelClient) ListLabels(ctx context.Context, owner, repo string, number int) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]string(nil), f.existing...), nil
}

func (f *fakeLabelClient) RemoveLabel(ctx context.Context, owner, repo string, number int, label string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, label)
	return nil
}

func TestPostSummaryReview_PostsFreshComment(t *testing.T) {
	f := &fakeSummaryClient{}
	err := PostSummaryReview(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "adds a widget"},
		model.Labels{},
		map[scoring.Severity]int{scoring.SeverityHigh: 1}, nil, nil, effort.Score{}, "")
	if err != nil {
		t.Fatalf("PostSummaryReview: %v", err)
	}
	if f.postCalls != 1 {
		t.Fatalf("PostIssueComment called %d, want 1", f.postCalls)
	}
	body := f.posted[0]
	if !strings.Contains(body, "adds a widget") {
		t.Errorf("walkthrough missing:\n%s", body)
	}
	if !strings.Contains(body, "| HIGH | 1 |") {
		t.Errorf("severity row missing:\n%s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), summaryFingerprint) {
		t.Errorf("fingerprint marker not at end:\n%s", body)
	}
}

// TestPostSummaryReview_ReplacesPriorMarkerCycle proves the delete-then-post
// cycle: first run posts fresh, second run finds the marker-tagged comment,
// deletes it, and posts again.
func TestPostSummaryReview_ReplacesPriorMarkerCycle(t *testing.T) {
	f := &fakeSummaryClient{
		existing: []gh.IssueComment{
			{ID: 100, Body: "human comment, keep me"},
		},
	}
	// First run.
	if err := PostSummaryReview(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "first"}, model.Labels{}, nil, nil, nil, effort.Score{}, ""); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if f.postCalls != 1 || len(f.deleted) != 0 {
		t.Fatalf("first run: posts=%d deletes=%d, want 1/0", f.postCalls, len(f.deleted))
	}
	firstID := f.existing[len(f.existing)-1].ID

	// Second run — should delete the first sacr comment but leave the human one.
	if err := PostSummaryReview(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "second"}, model.Labels{}, nil, nil, nil, effort.Score{}, ""); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if f.postCalls != 2 {
		t.Fatalf("posts=%d, want 2", f.postCalls)
	}
	if len(f.deleted) != 1 || f.deleted[0] != firstID {
		t.Errorf("expected delete of %d, got %v", firstID, f.deleted)
	}
	// Human comment survives.
	sawHuman := false
	for _, c := range f.existing {
		if c.ID == 100 {
			sawHuman = true
		}
	}
	if !sawHuman {
		t.Error("human comment was deleted; must only touch marker-tagged posts")
	}
}

func TestPostSummaryReview_ListErrorContinuesToPost(t *testing.T) {
	f := &fakeSummaryClient{listErr: errors.New("list boom")}
	err := PostSummaryReview(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "hi"}, model.Labels{}, nil, nil, nil, effort.Score{}, "")
	if err != nil {
		t.Fatalf("PostSummaryReview: %v", err)
	}
	if f.postCalls != 1 {
		t.Errorf("post should fire despite list error; posts=%d", f.postCalls)
	}
}

func TestPostSummaryReview_PostErrorPropagates(t *testing.T) {
	f := &fakeSummaryClient{postErr: errors.New("nope")}
	err := PostSummaryReview(context.Background(), f, "o", "r", 7,
		model.Summary{Walkthrough: "hi"}, model.Labels{}, nil, nil, nil, effort.Score{}, "")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("want wrapped post error, got %v", err)
	}
}

func TestPostSummaryReview_MissingIdentity(t *testing.T) {
	f := &fakeSummaryClient{}
	if err := PostSummaryReview(context.Background(), f, "", "r", 7, model.Summary{}, model.Labels{}, nil, nil, nil, effort.Score{}, ""); err == nil {
		t.Errorf("empty owner should error")
	}
	if err := PostSummaryReview(context.Background(), f, "o", "r", 0, model.Summary{}, model.Labels{}, nil, nil, nil, effort.Score{}, ""); err == nil {
		t.Errorf("zero pr should error")
	}
	if err := PostSummaryReview(context.Background(), nil, "o", "r", 7, model.Summary{}, model.Labels{}, nil, nil, nil, effort.Score{}, ""); err == nil {
		t.Errorf("nil client should error")
	}
}

func TestApplyLabels_AppliesLabels(t *testing.T) {
	f := &fakeLabelClient{}
	err := ApplyLabels(context.Background(), f, "o", "r", 7,
		model.Labels{PRType: "feat", RiskTag: "high", Domains: []string{"auth", "billing"}},
		map[scoring.Severity]int{}, nil)
	if err != nil {
		t.Fatalf("ApplyLabels: %v", err)
	}
	if f.addCalls != 1 {
		t.Fatalf("AddLabels calls = %d, want 1", f.addCalls)
	}
	got := strings.Join(f.lastLabels, ",")
	for _, want := range []string{"feat", "high", "auth", "billing"} {
		if !strings.Contains(got, want) {
			t.Errorf("labels missing %q: %v", want, f.lastLabels)
		}
	}
}

func TestApplyLabels_EmptyLabelSetSkipsAdd(t *testing.T) {
	f := &fakeLabelClient{}
	if err := ApplyLabels(context.Background(), f, "o", "r", 7, model.Labels{}, nil, nil); err != nil {
		t.Fatalf("ApplyLabels: %v", err)
	}
	if f.addCalls != 0 {
		t.Errorf("AddLabels called %d, want 0 for empty label set", f.addCalls)
	}
}

func TestApplyLabels_RemovesStaleExclusive(t *testing.T) {
	f := &fakeLabelClient{existing: []string{"risk/critical", "feat"}}
	err := ApplyLabels(context.Background(), f, "o", "r", 7,
		model.Labels{PRType: "fix", RiskTag: "risk/high"}, nil, nil)
	if err != nil {
		t.Fatalf("ApplyLabels: %v", err)
	}
	removedSet := make(map[string]bool)
	for _, r := range f.removed {
		removedSet[r] = true
	}
	if !removedSet["risk/critical"] || !removedSet["feat"] {
		t.Errorf("stale exclusive labels not removed: %v", f.removed)
	}
}

func TestApplyLabels_AddErrorPropagates(t *testing.T) {
	f := &fakeLabelClient{addErr: errors.New("nope")}
	err := ApplyLabels(context.Background(), f, "o", "r", 7,
		model.Labels{PRType: "feat"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("want wrapped add error, got %v", err)
	}
}

func TestApplyLabels_MissingIdentity(t *testing.T) {
	f := &fakeLabelClient{}
	if err := ApplyLabels(context.Background(), f, "", "r", 7, model.Labels{}, nil, nil); err == nil {
		t.Errorf("empty owner should error")
	}
	if err := ApplyLabels(context.Background(), nil, "o", "r", 7, model.Labels{}, nil, nil); err == nil {
		t.Errorf("nil client should error")
	}
}

func TestRenderSummary(t *testing.T) {
	block := RenderSummary(
		model.Summary{
			Walkthrough:  "does two things",
			ChangeGroups: []model.ChangeGroup{{Title: "Auth", Files: []string{"a.go"}, Summary: "rework"}},
			TestingNotes: "run go test",
		},
		model.Labels{RiskTag: "high"},
		map[scoring.Severity]int{scoring.SeverityCritical: 2, scoring.SeverityLow: 1},
		nil,
		nil,
		effort.Score{Value: 5, Dot: "🟡", Label: "medium",
			Contributions: []effort.Contribution{{Signal: "diff-size", Detail: "big", Points: 2.5}}},
		"")
	for _, want := range []string{
		"## Automated review by sacr",
		"### Reviewer effort",
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
