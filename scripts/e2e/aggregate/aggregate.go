// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 shubam-ai-code-reviewer contributors

// aggregate reads N per-case assert results (each `assert -json` output)
// and produces the E2E matrix summary: total recall, per-case table,
// pass/fail gate.
//
// Usage:
//
//	aggregate -in results/*.json [-min-recall 0.8] [-summary summary.json] [-markdown report.md]
//
// Exit codes:
//
//	0 — passed the gate (hard recall >= min-recall AND no missed critical)
//	1 — failed the gate (a real regression to look at)
//	2 — invalid input (couldn't parse a per-case file)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Result mirrors the shape emitted by `assert -json`. Duplicated here
// (small struct) rather than shared via an internal package because these
// scripts are intentionally isolated from the product's Go module graph.
type Result struct {
	Case            string   `json:"case"`
	Description     string   `json:"description,omitempty"`
	ExpectedTotal   int      `json:"expected_total"`
	ExpectedHard    int      `json:"expected_hard"`
	Matched         int      `json:"matched"`
	MatchedHard     int      `json:"matched_hard"`
	SoftMisses      []string `json:"soft_misses,omitempty"`
	HardMisses      []string `json:"hard_misses,omitempty"`
	ExtraFindings   int      `json:"extra_findings"`
	Recall          float64  `json:"recall"`
	HardRecall      float64  `json:"hard_recall"`
	Passed          bool     `json:"passed"`
	MissedCritical  bool     `json:"missed_critical"`
	CriticalSummary string   `json:"critical_summary,omitempty"`
}

// Summary is the aggregate view — what the report + workflow gate reads.
type Summary struct {
	Cases              int      `json:"cases"`
	CasesPassed        int      `json:"cases_passed"`
	CasesFailed        int      `json:"cases_failed"`
	ExpectedHardTotal  int      `json:"expected_hard_total"`
	MatchedHardTotal   int      `json:"matched_hard_total"`
	HardRecall         float64  `json:"hard_recall"`
	MinRecallThreshold float64  `json:"min_recall_threshold"`
	MissedCriticalIn   []string `json:"missed_critical_in,omitempty"`
	ExtraFindingsTotal int      `json:"extra_findings_total"`
	Passed             bool     `json:"passed"`
	Results            []Result `json:"results"`
}

// Aggregate computes the summary from N per-case results. Pure function.
func Aggregate(results []Result, minRecall float64) Summary {
	s := Summary{
		Cases:              len(results),
		MinRecallThreshold: minRecall,
	}
	for _, r := range results {
		if r.Passed {
			s.CasesPassed++
		} else {
			s.CasesFailed++
		}
		s.ExpectedHardTotal += r.ExpectedHard
		s.MatchedHardTotal += r.MatchedHard
		s.ExtraFindingsTotal += r.ExtraFindings
		if r.MissedCritical {
			s.MissedCriticalIn = append(s.MissedCriticalIn, r.Case)
		}
	}
	if s.ExpectedHardTotal > 0 {
		s.HardRecall = float64(s.MatchedHardTotal) / float64(s.ExpectedHardTotal)
	} else {
		s.HardRecall = 1.0
	}
	s.Passed = s.HardRecall >= s.MinRecallThreshold && len(s.MissedCriticalIn) == 0
	// Stable case order in the report — makes diffs across runs meaningful.
	sort.SliceStable(results, func(i, j int) bool { return results[i].Case < results[j].Case })
	s.Results = results
	return s
}

// --- CLI ---

func main() {
	minRecall := flag.Float64("min-recall", 0.8, "gate threshold on aggregate hard recall")
	summaryOut := flag.String("summary", "", "write summary JSON to this path (default: stdout)")
	markdownOut := flag.String("markdown", "", "write markdown report to this path")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "aggregate: no input files. usage: aggregate [-min-recall F] [-summary path] [-markdown path] result1.json result2.json ...")
		os.Exit(2)
	}

	results := make([]Result, 0, flag.NArg())
	for _, path := range flag.Args() {
		r, err := readResult(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aggregate: read %s: %v\n", path, err)
			os.Exit(2)
		}
		results = append(results, r)
	}
	s := Aggregate(results, *minRecall)

	// Summary JSON always emitted; goes to a file if -summary is set, else stdout.
	summaryData, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "aggregate: marshal summary: %v\n", err)
		os.Exit(2)
	}
	if *summaryOut != "" {
		if err := os.WriteFile(*summaryOut, summaryData, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "aggregate: write summary: %v\n", err)
			os.Exit(2)
		}
	} else {
		fmt.Fprintln(os.Stdout, string(summaryData))
	}

	if *markdownOut != "" {
		if err := os.WriteFile(*markdownOut, []byte(renderMarkdown(s)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "aggregate: write markdown: %v\n", err)
			os.Exit(2)
		}
	}

	if !s.Passed {
		os.Exit(1)
	}
}

func readResult(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return Result{}, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	if r.Case == "" {
		// Fall back to the file's basename so a mis-set case never disappears.
		r.Case = filepath.Base(path)
	}
	return r, nil
}

func renderMarkdown(s Summary) string {
	status := "✅ PASS"
	if !s.Passed {
		status = "❌ FAIL"
	}
	out := fmt.Sprintf("# sacr E2E matrix — %s\n\n", status)
	out += fmt.Sprintf("- Hard recall: **%.0f%%** (matched %d / expected %d, threshold %.0f%%)\n",
		s.HardRecall*100, s.MatchedHardTotal, s.ExpectedHardTotal, s.MinRecallThreshold*100)
	out += fmt.Sprintf("- Cases: %d passed / %d failed\n", s.CasesPassed, s.CasesFailed)
	out += fmt.Sprintf("- Extra findings across all cases: %d\n", s.ExtraFindingsTotal)
	if len(s.MissedCriticalIn) > 0 {
		out += fmt.Sprintf("- ⚠️ **Missed CRITICAL** in: %v\n", s.MissedCriticalIn)
	}
	out += "\n## Per-case\n\n"
	out += "| Case | Result | Hard recall | Extras | Notes |\n"
	out += "|---|---|---|---|---|\n"
	for _, r := range s.Results {
		icon := "✅"
		if !r.Passed {
			icon = "❌"
		}
		notes := ""
		if r.MissedCritical {
			notes = "missed CRITICAL"
		}
		if len(r.HardMisses) > 0 && notes == "" {
			notes = fmt.Sprintf("missed %d", len(r.HardMisses))
		}
		out += fmt.Sprintf("| %s | %s | %d/%d | %d | %s |\n",
			r.Case, icon, r.MatchedHard, r.ExpectedHard, r.ExtraFindings, notes)
	}
	return out
}
