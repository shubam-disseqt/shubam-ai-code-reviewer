// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package selector

import (
	"strings"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
)

func TestDefaultOptions(t *testing.T) {
	t.Parallel()
	opts := DefaultOptions()
	if opts.MaxTokensPerFile <= 0 {
		t.Fatalf("DefaultOptions.MaxTokensPerFile = %d; want > 0", opts.MaxTokensPerFile)
	}
	if opts.TokenCounter != nil {
		t.Fatal("DefaultOptions.TokenCounter should be nil so callers can plug in llm.CountTokens")
	}
}

func TestDefaultTokenCounter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"12345678", 2},
		{"123456789", 3},
	}
	for _, tc := range cases {
		if got := defaultTokenCounter(tc.in); got != tc.want {
			t.Fatalf("defaultTokenCounter(%q) = %d; want %d", tc.in, got, tc.want)
		}
	}
}

func TestSelectEmpty(t *testing.T) {
	t.Parallel()
	got := Select(nil, DefaultOptions())
	if len(got) != 0 {
		t.Fatalf("Select(nil) len = %d; want 0", len(got))
	}
	got = Select([]model.Diff{}, DefaultOptions())
	if len(got) != 0 {
		t.Fatalf("Select([]) len = %d; want 0", len(got))
	}
}

func TestSelectSingleSmallFile(t *testing.T) {
	t.Parallel()
	d := model.Diff{NewPath: "cmd/x/main.go", Diff: "@@ -0,0 +1 @@\n+package x\n"}
	got := Select([]model.Diff{d}, DefaultOptions())
	if len(got) != 1 {
		t.Fatalf("want 1 decision, got %d", len(got))
	}
	if !got[0].Included || got[0].Reason != ReasonNone {
		t.Fatalf("small go file should be included, got %+v", got[0])
	}
}

// TestSelectEveryInputProducesOneDecision is the load-bearing determinism
// property: no silent drops, one-to-one input→output, order preserved.
// Preview and the real dispatch depend on this to never disagree.
func TestSelectEveryInputProducesOneDecision(t *testing.T) {
	t.Parallel()
	inputs := []model.Diff{
		{NewPath: "cmd/a/main.go", Diff: "small"},
		{NewPath: "vendor/x/y.go", Diff: "small"},                  // default pattern
		{NewPath: "img.png", IsBinary: true},                       // binary
		{OldPath: "old.go", NewPath: "/dev/null", IsDeleted: true}, // deleted
		{NewPath: "README.md", Diff: "small"},                      // extension gate
		{NewPath: "pkg/big.go", Diff: strings.Repeat("x", 100000)}, // too large
		{NewPath: "pkg/small.go", Diff: "tiny"},                    // included
		{NewPath: "secret.go", Diff: "small"},                      // user exclude
		{NewPath: "docs/notes.txt", Diff: "small"},                 // extension miss (no include)
	}
	opts := Options{
		MaxTokensPerFile: 100,
		ExcludePatterns:  []string{"**/secret.go"},
		ExcludeDeleted:   true,
	}
	got := Select(inputs, opts)
	if len(got) != len(inputs) {
		t.Fatalf("decisions=%d; want %d — silent drop detected", len(got), len(inputs))
	}
	for i, dec := range got {
		if dec.Diff.NewPath != inputs[i].NewPath || dec.Diff.OldPath != inputs[i].OldPath {
			t.Fatalf("order not preserved at %d: got %+v want %+v", i, dec.Diff, inputs[i])
		}
		if dec.Included != (dec.Reason == ReasonNone) {
			t.Fatalf("index %d: Included/Reason disagree: %+v", i, dec)
		}
	}
	wantReasons := []ExclusionReason{
		ReasonNone,
		ReasonDefaultPattern,
		ReasonBinary,
		ReasonDeleted,
		ReasonExtension,
		ReasonTooLarge,
		ReasonNone,
		ReasonUserExclude,
		ReasonExtension,
	}
	for i, want := range wantReasons {
		if got[i].Reason != want {
			t.Fatalf("index %d reason = %q; want %q", i, got[i].Reason, want)
		}
	}
}

func TestSelectTooLargeUsesTokenCounter(t *testing.T) {
	t.Parallel()
	// Custom counter: always reports 1000 tokens regardless of input. With a
	// 500 cap, any go file trips ReasonTooLarge.
	opts := Options{
		MaxTokensPerFile: 500,
		TokenCounter:     func(string) int { return 1000 },
	}
	got := Select([]model.Diff{{NewPath: "a.go", Diff: "x"}}, opts)
	if got[0].Reason != ReasonTooLarge {
		t.Fatalf("reason = %q; want %q", got[0].Reason, ReasonTooLarge)
	}
}

func TestSelectMaxTokensZeroDisablesGate(t *testing.T) {
	t.Parallel()
	// Even with a gigantic diff, MaxTokensPerFile=0 skips the size gate.
	opts := DefaultOptions()
	opts.MaxTokensPerFile = 0
	huge := model.Diff{NewPath: "a.go", Diff: strings.Repeat("x", 1<<20)}
	got := Select([]model.Diff{huge}, opts)
	if !got[0].Included {
		t.Fatalf("size gate ran with cap=0: %+v", got[0])
	}
}

func TestSelectIncludePatternsShortCircuit(t *testing.T) {
	t.Parallel()
	// Both files are `.txt` (fail extension) but one matches includes.
	inputs := []model.Diff{
		{NewPath: "docs/keep.txt", Diff: "small"},
		{NewPath: "notes/drop.txt", Diff: "small"},
	}
	opts := Options{IncludePatterns: []string{"docs/**"}}
	got := Select(inputs, opts)
	if !got[0].Included {
		t.Fatalf("docs/keep.txt should pass via include: %+v", got[0])
	}
	if got[1].Reason != ReasonNotAllowed {
		t.Fatalf("notes/drop.txt want ReasonNotAllowed, got %q", got[1].Reason)
	}
}

func TestSelectDeterministic(t *testing.T) {
	t.Parallel()
	inputs := []model.Diff{
		{NewPath: "a.go", Diff: "one"},
		{NewPath: "vendor/b.go", Diff: "two"},
		{NewPath: "c.go", Diff: "three"},
	}
	opts := DefaultOptions()
	first := Select(inputs, opts)
	for i := 0; i < 5; i++ {
		next := Select(inputs, opts)
		if len(next) != len(first) {
			t.Fatalf("run %d length differs", i)
		}
		for j := range first {
			if first[j] != next[j] {
				t.Fatalf("run %d idx %d drifted: %+v vs %+v", i, j, first[j], next[j])
			}
		}
	}
}

func TestKept(t *testing.T) {
	t.Parallel()
	decs := []Decision{
		{Diff: model.Diff{NewPath: "a.go"}, Included: true, Reason: ReasonNone},
		{Diff: model.Diff{NewPath: "b.png"}, Included: false, Reason: ReasonBinary},
		{Diff: model.Diff{NewPath: "c.go"}, Included: true, Reason: ReasonNone},
	}
	kept := Kept(decs)
	if len(kept) != 2 {
		t.Fatalf("kept len = %d; want 2", len(kept))
	}
	if kept[0].NewPath != "a.go" || kept[1].NewPath != "c.go" {
		t.Fatalf("kept preserved order broken: %+v", kept)
	}
}

func TestKeptEmpty(t *testing.T) {
	t.Parallel()
	if got := Kept(nil); len(got) != 0 {
		t.Fatalf("Kept(nil) len = %d; want 0", len(got))
	}
}

func TestSummary(t *testing.T) {
	t.Parallel()
	decs := []Decision{
		{Reason: ReasonNone, Included: true},
		{Reason: ReasonNone, Included: true},
		{Reason: ReasonBinary},
		{Reason: ReasonBinary},
		{Reason: ReasonBinary},
		{Reason: ReasonTooLarge},
	}
	s := Summary(decs)
	if s[ReasonNone] != 2 {
		t.Fatalf("included=%d; want 2", s[ReasonNone])
	}
	if s[ReasonBinary] != 3 {
		t.Fatalf("binary=%d; want 3", s[ReasonBinary])
	}
	if s[ReasonTooLarge] != 1 {
		t.Fatalf("too_large=%d; want 1", s[ReasonTooLarge])
	}
	// Total must equal input length — no silent drops on the report side.
	total := 0
	for _, v := range s {
		total += v
	}
	if total != len(decs) {
		t.Fatalf("summary total = %d; want %d", total, len(decs))
	}
}
