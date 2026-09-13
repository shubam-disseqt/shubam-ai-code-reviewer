// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package sarif

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/shubam-disseqt/z-code-reviewer/internal/scoring"
)

// Finding is the emitter-facing input shape. Callers translate their own
// per-finding types (scanner findings, scored LLM comments) into this. Kept
// intentionally small — anything the schema needs beyond RuleID + location +
// severity is out of scope for v1.
type Finding struct {
	// Source is the producer key — e.g. "scanner:gitleaks" or "llm". Combined
	// with RuleID it deduplicates ReportingDescriptor entries and forms the
	// SARIF ruleId.
	Source string
	// RuleID is the tool-specific rule identifier.
	RuleID string
	// Description is a one-line human-readable summary; used both as the
	// ReportingDescriptor.shortDescription and the Result.message.
	Description string
	// Path is the repository-relative file path.
	Path string
	// StartLine / EndLine are 1-indexed. Zero StartLine emits no region.
	StartLine int
	EndLine   int
	// Severity is the Phase 16 bucketed severity; drives Result.level and the
	// rule's defaultConfiguration.level.
	Severity scoring.Severity
	// Fingerprint is the Phase 13 stable identity. Attached as
	// partialFingerprints["zreview/v1"] so GitHub Code Scanning can carry
	// alert state across pushes even when the diff line drifts.
	Fingerprint string
	// HelpURI optionally points at documentation for the rule.
	HelpURI string
}

// Meta names the run's containing repo and commit. Version comes from the
// caller (typically the ldflags-injected main.Version) so a build shows up
// in Code Scanning as itself, not "dev".
type Meta struct {
	Repo    string
	HeadSHA string
	Version string
}

// zreviewSchema is the published SARIF 2.1.0 JSON Schema URL. Optional in
// the spec but GitHub's SARIF validator surfaces a clearer error when it's
// present.
const zreviewSchema = "https://json.schemastore.org/sarif-2.1.0.json"

// toolInfoURI is the informationUri baked into every run.
const toolInfoURI = "https://github.com/shubam-disseqt/z-code-reviewer"

// Encode renders findings as SARIF 2.1.0 JSON. Output is deterministic:
// rules are sorted by ID, results preserve caller order. An empty findings
// slice still produces a valid Log with `results: []` (never null) so
// downstream consumers don't crash on the "clean review" case.
func Encode(findings []Finding, meta Meta) ([]byte, error) {
	rules := collectRules(findings)
	results := make([]Result, 0, len(findings))
	for _, f := range findings {
		if f.Severity == scoring.SeveritySuppress {
			// Belt-and-braces: emit stage should have dropped these already,
			// but if one leaks in we skip it silently rather than fabricate a
			// SARIF level for a suppressed finding.
			continue
		}
		results = append(results, findingToResult(f))
	}

	log := Log{
		Schema:  zreviewSchema,
		Version: "2.1.0",
		Runs: []Run{{
			Tool: Tool{
				Driver: ToolComponent{
					Name:           "zreview",
					Version:        meta.Version,
					InformationURI: toolInfoURI,
					Rules:          rules,
				},
			},
			Results: results,
		}},
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// SetEscapeHTML(false) keeps URIs and messages readable — SARIF is not
	// embedded in HTML and the escaping just produces noise on diffs.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(log); err != nil {
		return nil, fmt.Errorf("sarif encode: %w", err)
	}
	return buf.Bytes(), nil
}

// collectRules deduplicates findings by (Source, RuleID) → one
// ReportingDescriptor. Sorted by ID for stable golden diffs.
func collectRules(findings []Finding) []ReportingDescriptor {
	seen := make(map[string]ReportingDescriptor, len(findings))
	for _, f := range findings {
		id := sarifRuleID(f)
		if _, ok := seen[id]; ok {
			continue
		}
		desc := ReportingDescriptor{
			ID:   id,
			Name: f.RuleID,
			ShortDescription: MultiformatMessage{
				Text: shortDescription(f),
			},
		}
		if f.HelpURI != "" {
			desc.HelpURI = f.HelpURI
		}
		if lvl := severityToLevel(f.Severity); lvl != "" {
			desc.DefaultConfiguration = &ReportingConfiguration{Level: lvl}
		}
		seen[id] = desc
	}
	out := make([]ReportingDescriptor, 0, len(seen))
	for _, r := range seen {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// findingToResult renders one Finding as a Result.
func findingToResult(f Finding) Result {
	res := Result{
		RuleID:  sarifRuleID(f),
		Level:   severityToLevel(f.Severity),
		Message: MultiformatMessage{Text: f.Description},
	}
	if f.Path != "" {
		loc := Location{
			PhysicalLocation: PhysicalLocation{
				ArtifactLocation: ArtifactLocation{URI: f.Path},
			},
		}
		if f.StartLine > 0 {
			reg := &Region{StartLine: f.StartLine}
			if f.EndLine > f.StartLine {
				reg.EndLine = f.EndLine
			}
			loc.PhysicalLocation.Region = reg
		}
		res.Locations = []Location{loc}
	}
	if f.Fingerprint != "" {
		res.PartialFingerprints = map[string]string{
			"zreview/v1": f.Fingerprint,
		}
	}
	return res
}

// sarifRuleID composes a namespaced rule ID so gitleaks:aws-key doesn't
// collide with semgrep:aws-key. When Source is empty the RuleID is used
// verbatim.
func sarifRuleID(f Finding) string {
	if f.Source == "" {
		return f.RuleID
	}
	return f.Source + "/" + f.RuleID
}

// shortDescription defaults to the finding description; falls back to the
// rule id when the description is missing so the rule row is never blank.
func shortDescription(f Finding) string {
	if f.Description != "" {
		return f.Description
	}
	return f.RuleID
}

// severityToLevel maps Phase 16 severity to SARIF level. SUPPRESS returns
// "" — callers filter suppressed findings before Encode reaches them, but
// the mapping stays explicit so the routing rule from the roadmap is
// visible in one place.
func severityToLevel(s scoring.Severity) string {
	switch s {
	case scoring.SeverityCritical, scoring.SeverityHigh:
		return "error"
	case scoring.SeverityMedium:
		return "warning"
	case scoring.SeverityLow:
		return "note"
	case scoring.SeveritySuppress:
		return ""
	}
	// Unknown severity → surface as "warning" rather than crash. GitHub
	// rejects arbitrary levels, so this keeps the run acceptable.
	return "warning"
}
