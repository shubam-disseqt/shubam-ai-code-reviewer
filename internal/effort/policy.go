// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package effort

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed policy.yaml
var embeddedPolicyBytes []byte

// Policy holds the weights that turn Inputs into a Score. All fields have
// defaults from policy.yaml; override via env var or repo-local YAML.
type Policy struct {
	Base float64 `yaml:"base"`

	LOCChurn     SignalWeight `yaml:"loc_churn"`
	FilesChanged SignalWeight `yaml:"files_changed"`
	NewFiles     SignalWeight `yaml:"new_files"`
	MaxFileChurn SignalWeight `yaml:"max_file_churn"`

	TestRatio TestRatioWeight `yaml:"test_ratio"`
	Paths     PathWeights     `yaml:"paths"`
	Overlap   OverlapWeight   `yaml:"overlap"`
	Findings  FindingsWeight  `yaml:"findings"`

	source string
}

// SignalWeight is a per-unit weight with a per-signal cap.
type SignalWeight struct {
	PointsPerUnit float64 `yaml:"points_per_10_lines"`
	PointsPerFile float64 `yaml:"points_per_file"`
	Cap           float64 `yaml:"cap"`
}

// perUnit returns whichever field is set — YAML uses different key names
// for lines vs files but the math is identical.
func (s SignalWeight) perUnit() float64 {
	if s.PointsPerUnit != 0 {
		return s.PointsPerUnit
	}
	return s.PointsPerFile
}

// TestRatioWeight is a two-tier bonus. Negative points reduce the score.
type TestRatioWeight struct {
	BonusAt25 float64 `yaml:"bonus_at_25pct"`
	BonusAt50 float64 `yaml:"bonus_at_50pct"`
}

// PathWeights are flat contributions when the corresponding path bucket is
// touched. Set to 0 to disable an individual bucket.
type PathWeights struct {
	Auth      float64 `yaml:"auth"`
	Migration float64 `yaml:"migration"`
	Infra     float64 `yaml:"infra"`
	PublicAPI float64 `yaml:"public_api"`
}

// OverlapWeight scales the cross-PR overlap contribution.
type OverlapWeight struct {
	PointsPerPR float64 `yaml:"points_per_pr"`
	Cap         float64 `yaml:"cap"`
}

// FindingsWeight scales severity counts. Cap prevents a noisy PR from
// pinning the score to 10 on findings alone.
type FindingsWeight struct {
	Critical float64 `yaml:"points_per_critical"`
	High     float64 `yaml:"points_per_high"`
	Medium   float64 `yaml:"points_per_medium"`
	Low      float64 `yaml:"points_per_low"`
	Cap      float64 `yaml:"cap"`
}

// Source returns "embedded", "env(<path>)", or "repo(<path>)" so operators
// can see which policy actually loaded.
func (p Policy) Source() string { return p.source }

// LoadPolicy resolves the effort policy in override order:
//
//  1. $ZREVIEW_EFFORT_POLICY (absolute or relative to CWD)
//  2. <repoRoot>/.zreview/effort.yaml
//  3. embedded default
//
// A parse error on an override is surfaced — silently falling back to the
// default would mislead operators who thought their override applied.
func LoadPolicy(repoRoot string) (Policy, error) {
	if p := os.Getenv("ZREVIEW_EFFORT_POLICY"); p != "" {
		return loadFromFile(p, "env("+p+")")
	}
	repoPath := filepath.Join(repoRoot, ".zreview", "effort.yaml")
	if _, err := os.Stat(repoPath); err == nil {
		return loadFromFile(repoPath, "repo("+repoPath+")")
	}
	return loadFromBytes(embeddedPolicyBytes, "embedded")
}

func loadFromFile(path, source string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("effort policy: read %s: %w", path, err)
	}
	return loadFromBytes(data, source)
}

func loadFromBytes(data []byte, source string) (Policy, error) {
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Policy{}, fmt.Errorf("effort policy: parse %s: %w", source, err)
	}
	p.source = source
	return p, nil
}
