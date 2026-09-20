// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 shubam-ai-code-reviewer contributors

package main

import "testing"

// helper: build a minimal Actual from a shorthand.
func act(path, cat, sev string, start, end int) Actual {
	return Actual{Path: path, Category: cat, Severity: sev, StartLine: start, EndLine: end}
}
func exp(path, cat, sev string, start, end int) Expected {
	return Expected{Path: path, Category: cat, MinSeverity: sev, StartLine: start, EndLine: end}
}

func TestMatch_ExactLineExactCategory(t *testing.T) {
	if !Match(exp("a.go", "security", "high", 10, 12), act("a.go", "security", "high", 10, 12)) {
		t.Fatal("exact match should hit")
	}
}

func TestMatch_LineToleranceBothSides(t *testing.T) {
	// Expected 20-25, tolerance ±3 → window 17-28. Actual 27-27 → overlaps.
	if !Match(exp("a.go", "bug", "medium", 20, 25), act("a.go", "bug", "medium", 27, 27)) {
		t.Fatal("+2 line drift within tolerance should hit")
	}
	// Actual 15-16 → overlaps 17-28? no, 15-16 lies entirely below 17. Miss.
	if Match(exp("a.go", "bug", "medium", 20, 25), act("a.go", "bug", "medium", 15, 16)) {
		t.Fatal("-4 line drift beyond tolerance should miss")
	}
}

func TestMatch_CategoryCaseInsensitive(t *testing.T) {
	if !Match(exp("a.go", "SECURITY", "high", 10, 10), act("a.go", "security", "high", 10, 10)) {
		t.Fatal("category compare should be case-insensitive")
	}
}

func TestMatch_WrongCategoryMisses(t *testing.T) {
	if Match(exp("a.go", "security", "high", 10, 10), act("a.go", "bug", "high", 10, 10)) {
		t.Fatal("wrong category should miss")
	}
}

func TestMatch_WrongPathMisses(t *testing.T) {
	if Match(exp("a.go", "bug", "low", 10, 10), act("b.go", "bug", "low", 10, 10)) {
		t.Fatal("wrong path should miss")
	}
}

func TestMatch_SeverityGate(t *testing.T) {
	// Actual = low; expected min = medium. Should miss.
	if Match(exp("a.go", "bug", "medium", 10, 10), act("a.go", "bug", "low", 10, 10)) {
		t.Fatal("actual severity below floor should miss")
	}
	// Actual = high; expected min = medium. Should hit.
	if !Match(exp("a.go", "bug", "medium", 10, 10), act("a.go", "bug", "high", 10, 10)) {
		t.Fatal("actual severity above floor should hit")
	}
}

func TestAssert_AllMatched(t *testing.T) {
	e := ExpectedFile{
		Case: "seeded-basic",
		Findings: []Expected{
			exp("a.go", "security", "high", 10, 10),
			exp("b.go", "bug", "medium", 20, 22),
		},
	}
	a := ActualFile{
		Comments: []Actual{
			act("a.go", "security", "high", 10, 10),
			act("b.go", "bug", "medium", 21, 21),
		},
	}
	r := Assert("seeded-basic", e, a)
	if !r.Passed {
		t.Errorf("expected pass, got fail: %+v", r)
	}
	if r.Matched != 2 || r.MatchedHard != 2 {
		t.Errorf("expected 2 matches, got %d", r.Matched)
	}
	if r.ExtraFindings != 0 {
		t.Errorf("expected 0 extras, got %d", r.ExtraFindings)
	}
	if r.HardRecall != 1.0 {
		t.Errorf("expected 100%% recall, got %.2f", r.HardRecall)
	}
}

func TestAssert_HardMissFails(t *testing.T) {
	e := ExpectedFile{
		Case: "one-missed",
		Findings: []Expected{
			exp("a.go", "security", "high", 10, 10),
			exp("b.go", "bug", "medium", 20, 22),
		},
	}
	// Only the first expected is present.
	a := ActualFile{Comments: []Actual{act("a.go", "security", "high", 10, 10)}}
	r := Assert("one-missed", e, a)
	if r.Passed {
		t.Errorf("expected fail on 50%% recall, got pass")
	}
	if len(r.HardMisses) != 1 {
		t.Errorf("expected 1 hard miss, got %d", len(r.HardMisses))
	}
	if r.HardRecall != 0.5 {
		t.Errorf("expected 0.5 hard recall, got %.2f", r.HardRecall)
	}
}

func TestAssert_SoftMissDoesNotFail(t *testing.T) {
	e := ExpectedFile{
		Case: "soft-only",
		Findings: []Expected{
			exp("a.go", "security", "high", 10, 10),
			{Path: "b.go", Category: "maintainability", MinSeverity: "low", StartLine: 30, EndLine: 30, Soft: true},
		},
	}
	// Only the hard finding is present.
	a := ActualFile{Comments: []Actual{act("a.go", "security", "high", 10, 10)}}
	r := Assert("soft-only", e, a)
	if !r.Passed {
		t.Errorf("expected pass — soft miss shouldn't fail, got %+v", r)
	}
	if len(r.SoftMisses) != 1 {
		t.Errorf("expected 1 soft miss, got %d", len(r.SoftMisses))
	}
	if len(r.HardMisses) != 0 {
		t.Errorf("expected 0 hard misses, got %d", len(r.HardMisses))
	}
}

func TestAssert_MissedCriticalMarksSpecial(t *testing.T) {
	e := ExpectedFile{
		Case:     "critical-missed",
		Findings: []Expected{exp("cfg.go", "security", "critical", 5, 5)},
	}
	a := ActualFile{Comments: nil}
	r := Assert("critical-missed", e, a)
	if !r.MissedCritical {
		t.Fatal("expected MissedCritical=true")
	}
	if r.Passed {
		t.Fatal("expected Passed=false when a critical is missed")
	}
	if r.CriticalSummary == "" {
		t.Fatal("expected non-empty CriticalSummary")
	}
}

func TestAssert_ExtraFindingsCountedButNoFail(t *testing.T) {
	e := ExpectedFile{Case: "just-one", Findings: []Expected{exp("a.go", "bug", "medium", 10, 10)}}
	a := ActualFile{
		Comments: []Actual{
			act("a.go", "bug", "medium", 10, 10),
			act("z.go", "style", "low", 100, 100), // noise; no expected match
		},
	}
	r := Assert("just-one", e, a)
	if !r.Passed {
		t.Fatal("expected pass despite extra")
	}
	if r.ExtraFindings != 1 {
		t.Errorf("expected 1 extra, got %d", r.ExtraFindings)
	}
}

func TestAssert_EmptyExpectedPassesOnEmptyActual(t *testing.T) {
	r := Assert("docs-only", ExpectedFile{}, ActualFile{})
	if !r.Passed {
		t.Fatal("empty in, empty out should pass")
	}
	if r.HardRecall != 1.0 {
		t.Errorf("empty expected → HardRecall should be 1.0 for cleanliness, got %.2f", r.HardRecall)
	}
}

func TestAssert_EmptyExpectedWithExtrasStillPasses(t *testing.T) {
	// A docs-only PR that seeds nothing but the LLM emits an unrelated nit.
	// Precision drops but the case still passes; extras only inform the report.
	r := Assert("docs-only", ExpectedFile{}, ActualFile{Comments: []Actual{
		act("README.md", "style", "low", 1, 1),
	}})
	if !r.Passed {
		t.Fatal("should pass — noise doesn't fail an empty-expected case")
	}
	if r.ExtraFindings != 1 {
		t.Errorf("expected 1 extra, got %d", r.ExtraFindings)
	}
}
