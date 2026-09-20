// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scoring

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyEntry is the per-category (or per-rule) scoring policy. All three
// fields are required in the on-disk YAML — the loader validates.
type PolicyEntry struct {
	Impact          float64             `yaml:"impact"`
	ConfidenceFloor float64             `yaml:"confidence_floor"`
	SeverityMap     map[string]Severity `yaml:"severity_map"`
}

// Policy is the loaded scoring policy. Categories keys accept two shapes:
// a fully qualified "category.rule" (e.g. "security.hardcoded-secret") or
// a bare "category" (e.g. "security"). Lookup falls back from specific to
// generic to Defaults.
type Policy struct {
	Categories map[string]PolicyEntry `yaml:"categories"`
	Defaults   PolicyEntry            `yaml:"defaults"`

	// source names where the policy was loaded from — env var path,
	// repo-local override, or "embedded". Used for the review log line.
	source string
}

// Source returns a human-readable label for where the policy was loaded
// from. Empty on a zero-value Policy.
func (p Policy) Source() string { return p.source }

// defaultPolicyYAML is the embedded fallback. Ships with entries for the
// scanner-produced categories (secret, sast, cve), the LLM buckets (bug,
// security, performance, maintainability, style, documentation, test),
// and the four PDF §6 examples as concrete rule entries.
//
//go:embed policy.yaml
var defaultPolicyYAML []byte

// LoadPolicy resolves the effective policy in the documented order:
//
//  1. $SACR_SCORING_POLICY (path to a YAML file, if set and non-empty)
//  2. <repoRoot>/.sacr/scoring.yaml (if it exists)
//  3. embedded default (always succeeds)
//
// repoRoot may be empty; in that case the repo-local step is skipped.
// The first source that yields a parseable Policy wins. A parse error
// on an explicitly configured source returns the error rather than
// silently falling through — the operator asked for that file.
func LoadPolicy(repoRoot string) (Policy, error) {
	if p := os.Getenv("SACR_SCORING_POLICY"); p != "" {
		pol, err := loadPolicyFile(p)
		if err != nil {
			return Policy{}, fmt.Errorf("scoring: load %s: %w", p, err)
		}
		pol.source = p
		return pol, nil
	}
	if repoRoot != "" {
		local := filepath.Join(repoRoot, ".sacr", "scoring.yaml")
		if _, err := os.Stat(local); err == nil {
			pol, err := loadPolicyFile(local)
			if err != nil {
				return Policy{}, fmt.Errorf("scoring: load %s: %w", local, err)
			}
			pol.source = local
			return pol, nil
		}
	}
	pol, err := parsePolicy(defaultPolicyYAML)
	if err != nil {
		// This is a programming error — the embedded YAML must parse.
		return Policy{}, fmt.Errorf("scoring: parse embedded policy: %w", err)
	}
	pol.source = "embedded"
	return pol, nil
}

// loadPolicyFile reads a YAML file from disk and parses it into a Policy.
func loadPolicyFile(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	return parsePolicy(data)
}

// parsePolicy decodes YAML bytes into a Policy and validates the fields
// callers rely on. Individual entries may leave severity_map empty (they
// then fall back to Defaults) but a policy must have a non-empty
// Defaults.SeverityMap or nothing will ever match.
func parsePolicy(data []byte) (Policy, error) {
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Policy{}, fmt.Errorf("yaml: %w", err)
	}
	// Normalise: lowercase severity_map keys, uppercase severity values.
	// YAML is loose about case; this makes lookup deterministic.
	p.Defaults.SeverityMap = normaliseSeverityMap(p.Defaults.SeverityMap)
	if p.Defaults.SeverityMap == nil || len(p.Defaults.SeverityMap) == 0 {
		return Policy{}, fmt.Errorf("defaults.severity_map is required")
	}
	if _, ok := p.Defaults.SeverityMap["default"]; !ok {
		return Policy{}, fmt.Errorf("defaults.severity_map must contain a 'default' key")
	}
	for k, entry := range p.Categories {
		entry.SeverityMap = normaliseSeverityMap(entry.SeverityMap)
		p.Categories[k] = entry
		_ = k
	}
	return p, nil
}

// normaliseSeverityMap lowercases keys (they're raw severity labels) and
// uppercases severity values so scoring.go can compare them without
// worrying about case. Returns a fresh map so the caller doesn't share
// state with the yaml decoder's map.
func normaliseSeverityMap(in map[string]Severity) map[string]Severity {
	if in == nil {
		return nil
	}
	out := make(map[string]Severity, len(in))
	for k, v := range in {
		out[strings.ToLower(strings.TrimSpace(k))] = Severity(strings.ToUpper(strings.TrimSpace(string(v))))
	}
	return out
}
