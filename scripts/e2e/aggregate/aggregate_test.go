// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 shubam-ai-code-reviewer contributors

package main

import "testing"

func mkResult(name string, passed bool, expected, matched, extras int) Result {
	r := Result{
		Case:          name,
		ExpectedHard:  expected,
		MatchedHard:   matched,
		ExpectedTotal: expected,
		Matched:       matched,
		ExtraFindings: extras,
		Passed:        passed,
	}
	if expected > 0 {
		r.HardRecall = float64(matched) / float64(expected)
	} else {
		r.HardRecall = 1.0
	}
	return r
}

func TestAggregate_AllPass(t *testing.T) {
	s := Aggregate([]Result{
		mkResult("secret", true, 1, 1, 0),
		mkResult("sql", true, 2, 2, 1),
	}, 0.8)
	if !s.Passed {
		t.Errorf("expected pass at 100%% recall, got fail: %+v", s)
	}
	if s.HardRecall != 1.0 {
		t.Errorf("expected 1.0, got %.2f", s.HardRecall)
	}
	if s.ExtraFindingsTotal != 1 {
		t.Errorf("extras should sum: expected 1, got %d", s.ExtraFindingsTotal)
	}
}

func TestAggregate_BelowThresholdFails(t *testing.T) {
	// 2/4 hard = 50% aggregate recall; threshold 80% → fail.
	s := Aggregate([]Result{
		mkResult("a", true, 2, 2, 0),
		mkResult("b", false, 2, 0, 0),
	}, 0.8)
	if s.Passed {
		t.Fatal("expected fail: 50%% < 80%%")
	}
	if s.HardRecall != 0.5 {
		t.Errorf("expected 0.5, got %.2f", s.HardRecall)
	}
}

func TestAggregate_AtThresholdPasses(t *testing.T) {
	// 4/5 hard = 80% aggregate recall; threshold 80% → pass.
	s := Aggregate([]Result{
		mkResult("a", true, 2, 2, 0),
		mkResult("b", true, 3, 2, 0),
	}, 0.8)
	if !s.Passed {
		t.Fatalf("expected pass: 80%% == 80%%, got %+v", s)
	}
}

func TestAggregate_MissedCriticalFailsEvenAboveThreshold(t *testing.T) {
	r := mkResult("secret", false, 1, 0, 0)
	r.MissedCritical = true
	r.CriticalSummary = "config.go:5-5 [security/critical]"
	s := Aggregate([]Result{
		r,
		mkResult("a", true, 20, 20, 0), // fluff to push recall above threshold
	}, 0.8)
	if s.Passed {
		t.Fatal("missed critical should fail the whole matrix")
	}
	if len(s.MissedCriticalIn) != 1 || s.MissedCriticalIn[0] != "secret" {
		t.Errorf("expected MissedCriticalIn=[secret], got %v", s.MissedCriticalIn)
	}
}

func TestAggregate_EmptyExpectedResultsPass(t *testing.T) {
	// A docs-only case that seeds nothing shouldn't drop aggregate recall.
	s := Aggregate([]Result{
		mkResult("docs", true, 0, 0, 2), // 0 expected, 2 noise findings
		mkResult("real", true, 3, 3, 0),
	}, 0.8)
	if !s.Passed {
		t.Fatal("docs-only + all-real-matched should pass")
	}
	if s.HardRecall != 1.0 {
		t.Errorf("expected 1.0 (3/3), got %.2f", s.HardRecall)
	}
	if s.ExtraFindingsTotal != 2 {
		t.Errorf("expected 2 total extras, got %d", s.ExtraFindingsTotal)
	}
}

func TestAggregate_ZeroHardExpectedYields100(t *testing.T) {
	// If every case in the matrix is docs-only, hard recall is 1.0 (100% of 0).
	s := Aggregate([]Result{mkResult("docs", true, 0, 0, 0)}, 0.8)
	if s.HardRecall != 1.0 {
		t.Errorf("expected 1.0 when nothing to match, got %.2f", s.HardRecall)
	}
	if !s.Passed {
		t.Fatal("zero-expected matrix should pass")
	}
}
