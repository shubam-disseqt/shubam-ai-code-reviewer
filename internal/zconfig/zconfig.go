// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package zconfig loads the user-facing zreview config file at
// <repo>/.zreview/config.yaml. Unlike scoring/effort policies this file
// carries operator toggles (suggestions on/off, blocking or not) rather
// than tuning weights, so a malformed file falls back to defaults with
// a wrapped error the caller can log-and-continue on.
package zconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the parsed .zreview/config.yaml. Add new sections as
// optional structs so old files keep loading.
type Config struct {
	Suggestions SuggestionsConfig `yaml:"suggestions"`
}

// SuggestionsConfig controls the proactive suggestion pipeline.
type SuggestionsConfig struct {
	Enabled  bool `yaml:"enabled"`
	Blocking bool `yaml:"blocking"`
}

// Default returns the built-in defaults used when no config file is
// present: suggestions on, non-blocking.
func Default() Config {
	return Config{
		Suggestions: SuggestionsConfig{Enabled: true, Blocking: false},
	}
}

// Load reads <repo>/.zreview/config.yaml.
//
// Contract:
//   - missing file → Default(), nil
//   - empty file   → Default(), nil
//   - malformed    → Default(), wrapped error (caller may log-and-continue)
//   - valid        → parsed Config, nil (unspecified fields keep Default values)
//
// repo may be empty; that skips the file lookup entirely.
func Load(repo string) (Config, error) {
	def := Default()
	if repo == "" {
		return def, nil
	}
	path := filepath.Join(repo, ".zreview", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return def, nil
		}
		return def, fmt.Errorf("zconfig: read %s: %w", path, err)
	}
	if len(data) == 0 {
		return def, nil
	}
	// Start from defaults so partial files (only `suggestions.blocking`
	// set, say) don't zero out unspecified fields.
	cfg := def
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return def, fmt.Errorf("zconfig: parse %s: %w", path, err)
	}
	return cfg, nil
}
