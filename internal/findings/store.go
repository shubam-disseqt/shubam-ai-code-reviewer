// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package findings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultDir returns the on-disk root for the per-PR JSON files. The env var
// mirrors SACR_SESSION_DIR (same convention warmer.go uses) so tests and
// self-hosted CI can redirect it. Callers that already have a dir should
// pass it directly to Load / Save.
func DefaultDir() (string, error) {
	if d := os.Getenv("SACR_FINDINGS_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve findings dir: %w", err)
	}
	return filepath.Join(home, ".sacr", "findings"), nil
}

// filePath returns the canonical JSON file for a (owner, repo, pr) tuple.
func filePath(dir, owner, repo string, pr int) string {
	return filepath.Join(dir, fmt.Sprintf("%s_%s_%d.json", owner, repo, pr))
}

// Load returns the persisted findings for this PR. A missing file is not an
// error — a first-review PR has no prior state and returns an empty slice.
func Load(dir, owner, repo string, pr int) ([]Finding, error) {
	data, err := os.ReadFile(filePath(dir, owner, repo, pr))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read findings: %w", err)
	}
	var out []Finding
	if len(data) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal findings: %w", err)
	}
	return out, nil
}

// Save writes findings atomically: JSON to <file>.tmp, then rename. That
// keeps readers from seeing a truncated file if the writer crashes.
func Save(dir, owner, repo string, pr int, findings []Finding) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir findings dir: %w", err)
	}
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal findings: %w", err)
	}
	final := filePath(dir, owner, repo, pr)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp findings: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename findings: %w", err)
	}
	return nil
}
