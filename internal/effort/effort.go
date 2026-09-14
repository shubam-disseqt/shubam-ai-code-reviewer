// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package effort

import (
	"fmt"
	"math"
	"strings"
)

// Compute turns Inputs into a Score, deterministically. Same inputs +
// same policy → same output every time. Every contribution is captured so
// the caller can render an audit table.
func Compute(in Inputs, p Policy) Score {
	var s Score
	s.Contributions = make([]Contribution, 0, 12)

	add := func(signal, detail string, pts float64, capped bool) {
		if pts == 0 && !capped {
			return
		}
		s.Contributions = append(s.Contributions, Contribution{
			Signal: signal, Detail: detail, Points: pts, Capped: capped,
		})
		s.Raw += pts
	}

	if p.Base > 0 {
		add("Base", "", p.Base, false)
	}

	// --- Diff-shape signals ---
	totalAdded, totalDeleted, maxChurn, maxChurnPath := 0, 0, 0, ""
	newFiles := 0
	testFiles, codeFiles := 0, 0
	for _, f := range in.Files {
		totalAdded += f.LinesAdded
		totalDeleted += f.LinesDeleted
		if f.IsNew {
			newFiles++
		}
		churn := f.LinesAdded + f.LinesDeleted
		if churn > maxChurn {
			maxChurn = churn
			maxChurnPath = f.Path
		}
		if f.IsTest {
			testFiles++
		} else {
			codeFiles++
		}
	}
	totalChurn := totalAdded + totalDeleted

	addWithCap := func(signal, detail string, raw, cap float64) {
		if cap > 0 && raw >= cap {
			add(signal, detail, cap, true)
			return
		}
		add(signal, detail, raw, false)
	}

	if p.LOCChurn.perUnit() > 0 && totalChurn > 0 {
		raw := float64(totalChurn) / 10.0 * p.LOCChurn.perUnit()
		addWithCap("LOC churn",
			fmt.Sprintf("+%d / −%d (%d lines)", totalAdded, totalDeleted, totalChurn),
			raw, p.LOCChurn.Cap)
	}
	if p.FilesChanged.perUnit() > 0 && len(in.Files) > 0 {
		raw := float64(len(in.Files)) * p.FilesChanged.perUnit()
		addWithCap("Files changed",
			fmt.Sprintf("%d file(s)", len(in.Files)), raw, p.FilesChanged.Cap)
	}
	if p.NewFiles.perUnit() > 0 && newFiles > 0 {
		raw := float64(newFiles) * p.NewFiles.perUnit()
		addWithCap("New files",
			fmt.Sprintf("%d new", newFiles), raw, p.NewFiles.Cap)
	}
	if p.MaxFileChurn.perUnit() > 0 && maxChurn > 0 {
		raw := float64(maxChurn) / 10.0 * p.MaxFileChurn.perUnit()
		addWithCap("Largest single file",
			fmt.Sprintf("%d lines in %s", maxChurn, maxChurnPath),
			raw, p.MaxFileChurn.Cap)
	}

	// --- Test ratio (negative contribution) ---
	if codeFiles > 0 && (p.TestRatio.BonusAt25 < 0 || p.TestRatio.BonusAt50 < 0) {
		total := codeFiles + testFiles
		ratio := float64(testFiles) / float64(total)
		switch {
		case ratio >= 0.5 && p.TestRatio.BonusAt50 < 0:
			add("Test file ratio",
				fmt.Sprintf("%.0f%% (%d test / %d total)", ratio*100, testFiles, total),
				p.TestRatio.BonusAt50, false)
		case ratio >= 0.25 && p.TestRatio.BonusAt25 < 0:
			add("Test file ratio",
				fmt.Sprintf("%.0f%% (%d test / %d total)", ratio*100, testFiles, total),
				p.TestRatio.BonusAt25, false)
		}
	}

	// --- Path-touched signals ---
	if p.Paths.Auth > 0 && TouchesAuth(in.Files) {
		add("Auth path touched", firstMatchingPath(in.Files, TouchesAuthFile), p.Paths.Auth, false)
	}
	if p.Paths.Migration > 0 && TouchesMigration(in.Files) {
		add("Migration path touched", firstMatchingPath(in.Files, touchesMigrationFile), p.Paths.Migration, false)
	}
	if p.Paths.Infra > 0 && TouchesInfra(in.Files) {
		add("Infra path touched", firstMatchingPath(in.Files, touchesInfraFile), p.Paths.Infra, false)
	}

	// --- Overlap ---
	if p.Overlap.PointsPerPR > 0 && in.OverlappingPRs > 0 {
		raw := float64(in.OverlappingPRs) * p.Overlap.PointsPerPR
		addWithCap("Overlapping PRs",
			fmt.Sprintf("%d other PR(s)", in.OverlappingPRs), raw, p.Overlap.Cap)
	}

	// --- Findings ---
	findingsRaw := 0.0
	if p.Findings.Critical > 0 {
		findingsRaw += float64(in.FindingsBySev["CRITICAL"]) * p.Findings.Critical
	}
	if p.Findings.High > 0 {
		findingsRaw += float64(in.FindingsBySev["HIGH"]) * p.Findings.High
	}
	if p.Findings.Medium > 0 {
		findingsRaw += float64(in.FindingsBySev["MEDIUM"]) * p.Findings.Medium
	}
	if p.Findings.Low > 0 {
		findingsRaw += float64(in.FindingsBySev["LOW"]) * p.Findings.Low
	}
	if findingsRaw > 0 {
		addWithCap("Findings",
			fmt.Sprintf("%d CRIT / %d HIGH / %d MED / %d LOW",
				in.FindingsBySev["CRITICAL"], in.FindingsBySev["HIGH"],
				in.FindingsBySev["MEDIUM"], in.FindingsBySev["LOW"]),
			findingsRaw, p.Findings.Cap)
	}

	// --- Clamp + round + label ---
	if s.Raw < 0 {
		s.Raw = 0
	}
	if s.Raw > 10 {
		s.Raw = 10
	}
	s.Value = int(math.Round(s.Raw))
	s.Label, s.Dot = labelAndDot(s.Value)
	return s
}

// labelAndDot maps a 0-10 integer to a human label + traffic-light emoji.
func labelAndDot(v int) (label, dot string) {
	switch {
	case v <= 2:
		return "trivial", "🟢"
	case v <= 4:
		return "light", "🟢"
	case v <= 6:
		return "medium", "🟡"
	case v <= 8:
		return "medium-high", "🟡"
	default:
		return "heavy", "🔴"
	}
}

// firstMatchingPath returns the first file whose path matches predicate, so
// the audit table can name a concrete example.
func firstMatchingPath(files []FileDelta, match func(string) bool) string {
	var hits []string
	for _, f := range files {
		if match(f.Path) {
			hits = append(hits, f.Path)
		}
	}
	if len(hits) == 0 {
		return ""
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return hits[0] + " (+" + fmt.Sprintf("%d", len(hits)-1) + " more)"
}

// TouchesAuthFile is the single-file predicate variant so the audit table
// can show which file triggered the bucket.
func TouchesAuthFile(path string) bool {
	return anyComponentMatches(path, authPathKeywords)
}

func touchesMigrationFile(path string) bool {
	if anyComponentMatches(path, migrationPathKeywords) {
		return true
	}
	return strings.EqualFold(pathExt(path), ".sql")
}

func touchesInfraFile(path string) bool {
	base := strings.ToLower(baseName(path))
	if base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") {
		return true
	}
	return anyComponentMatches(path, infraPathKeywords)
}

// pathExt / baseName are thin wrappers to keep this file free of a
// filepath import when it doesn't need one otherwise.
func pathExt(p string) string {
	i := strings.LastIndexByte(p, '.')
	if i < 0 {
		return ""
	}
	return p[i:]
}

func baseName(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return p
	}
	return p[i+1:]
}
