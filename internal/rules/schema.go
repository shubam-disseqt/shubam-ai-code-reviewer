// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package rules loads org-level review rules from a git-hosted rules
// repo, filters them by scope + path glob, and renders the injectable
// "## Custom Review Rules" block for the review prompt.
package rules

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Rule is an org-level review rule authored in YAML.
type Rule struct {
	ID           string   `yaml:"id"`
	Title        string   `yaml:"title"`
	Body         string   `yaml:"body"`
	Scope        string   `yaml:"scope"`         // "global" | "repo" | "path"
	Repos        []string `yaml:"repos"`         // required when Scope=="repo"; owner/name form
	Paths        []string `yaml:"paths"`         // globs; required when Scope=="path"
	ExcludePaths []string `yaml:"exclude_paths"` //nolint:tagliatelle
	Severity     string   `yaml:"severity"`      // "blocker" | "warning" | "suggestion" | "nitpick"
	Category     string   `yaml:"category"`
	Enabled      *bool    `yaml:"enabled"` // pointer so we can distinguish absent (default true) from explicit false
	SourcePath   string   `yaml:"-"`       // populated by loader for diagnostics
}

// validScopes lists the recognised Scope values.
var validScopes = map[string]struct{}{
	"global": {},
	"repo":   {},
	"path":   {},
}

// validSeverities lists the recognised Severity values. Empty is allowed
// (category-only rules may omit it).
var validSeverities = map[string]struct{}{
	"":           {},
	"blocker":    {},
	"warning":    {},
	"suggestion": {},
	"nitpick":    {},
}

// multiDoc is the wrapper shape for a YAML file that contains a list of rules
// under the top-level `rules:` key.
type multiDoc struct {
	Rules []Rule `yaml:"rules"`
}

// Parse decodes YAML content into one or more rules and validates them.
//
// A file may hold ONE rule (top-level keyed by `id`, `title`, ...) or a
// list under `rules:` (multi-doc). Both forms are accepted; missing
// required fields fail loud.
func Parse(content []byte, sourcePath string) ([]Rule, error) {
	if len(content) == 0 {
		return nil, fmt.Errorf("rules: empty file %s", sourcePath)
	}

	// Try multi-doc first (list under `rules:`).
	var multi multiDoc
	if err := yaml.Unmarshal(content, &multi); err == nil && len(multi.Rules) > 0 {
		return finalize(multi.Rules, sourcePath)
	}

	// Fall back to single-doc.
	var single Rule
	if err := yaml.Unmarshal(content, &single); err != nil {
		return nil, fmt.Errorf("rules: parse %s: %w", sourcePath, err)
	}
	if single.ID == "" && single.Title == "" && single.Scope == "" {
		return nil, fmt.Errorf("rules: %s contains no rule (expected top-level id/title/scope or a `rules:` list)", sourcePath)
	}
	return finalize([]Rule{single}, sourcePath)
}

// finalize applies defaults (Enabled=true if absent), stamps SourcePath, and
// validates each rule.
func finalize(rs []Rule, sourcePath string) ([]Rule, error) {
	out := make([]Rule, 0, len(rs))
	for i := range rs {
		r := rs[i]
		if r.Enabled == nil {
			t := true
			r.Enabled = &t
		}
		r.SourcePath = sourcePath
		if err := validate(r); err != nil {
			return nil, fmt.Errorf("rules: %s: %w", sourcePath, err)
		}
		out = append(out, r)
	}
	return out, nil
}

func validate(r Rule) error {
	if r.ID == "" {
		return fmt.Errorf("missing required field: id")
	}
	if r.Title == "" {
		return fmt.Errorf("rule %q: missing required field: title", r.ID)
	}
	if r.Body == "" {
		return fmt.Errorf("rule %q: missing required field: body", r.ID)
	}
	if r.Scope == "" {
		return fmt.Errorf("rule %q: missing required field: scope", r.ID)
	}
	if _, ok := validScopes[r.Scope]; !ok {
		return fmt.Errorf("rule %q: invalid scope %q (want global|repo|path)", r.ID, r.Scope)
	}
	if _, ok := validSeverities[r.Severity]; !ok {
		return fmt.Errorf("rule %q: invalid severity %q (want blocker|warning|suggestion|nitpick)", r.ID, r.Severity)
	}
	if r.Scope == "repo" && len(r.Repos) == 0 {
		return fmt.Errorf("rule %q: scope=repo requires non-empty repos", r.ID)
	}
	if r.Scope == "path" && len(r.Paths) == 0 {
		return fmt.Errorf("rule %q: scope=path requires non-empty paths", r.ID)
	}
	return nil
}

// IsEnabled is a nil-safe accessor for the Enabled pointer.
func (r Rule) IsEnabled() bool {
	return r.Enabled == nil || *r.Enabled
}
