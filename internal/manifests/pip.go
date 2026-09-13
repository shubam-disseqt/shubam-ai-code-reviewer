// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/manifests.py under Apache License 2.0.

package manifests

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// ── requirements*.txt ─────────────────────────────────────────────────────────

type requirementsTxtParser struct{}

func (requirementsTxtParser) Match(path string) bool {
	base := baseName(path)
	// requirements.txt, requirements-dev.txt, requirements-anything.txt
	if !strings.HasSuffix(base, ".txt") {
		return false
	}
	name := strings.TrimSuffix(base, ".txt")
	return name == "requirements" || strings.HasPrefix(name, "requirements")
}

// pipSpec matches `name` + optional operator + optional version.
// Accepts: requests==2.31.0, django>=4.2,<5.0, numpy ~= 1.26, bare `flask`.
var (
	pipSpec      = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9\-_.]*)\s*([=<>!~]=?|===)?\s*([^;#\s]*)`)
	pipExtrasRe  = regexp.MustCompile(`\[[^\]]*\]`)
	pep508NameRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9\-_.]*)\s*(.*)$`)
)

func (requirementsTxtParser) Parse(content, filePath string) ([]Package, error) {
	var out []Package
	lower := strings.ToLower(filePath)
	isDev := strings.Contains(lower, "dev") || strings.Contains(lower, "test")
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Skip comments, options (-e, -r), and path installs.
		switch line[0] {
		case '#', '-', '.', '/':
			continue
		}
		// Strip trailing comment.
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		// Strip extras: requests[security]==2.31.0 → requests==2.31.0.
		line = pipExtrasRe.ReplaceAllString(line, "")
		m := pipSpec.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		op := m[2]
		ver := strings.TrimSpace(m[3])
		constraint := ""
		if ver != "" {
			constraint = op + ver
		}
		out = append(out, Package{
			Name:     name,
			Kind:     "pip",
			Version:  constraint,
			FilePath: filePath,
			IsDev:    isDev,
		})
	}
	return out, nil
}

// splitPEP508 turns "requests>=2.31.0; python_version>=\"3.8\"" into
// ("requests", ">=2.31.0"). Empty name signals a rejected spec.
func splitPEP508(spec string) (string, string) {
	spec = strings.SplitN(spec, ";", 2)[0]
	spec = strings.TrimSpace(spec)
	spec = pipExtrasRe.ReplaceAllString(spec, "")
	m := pep508NameRe.FindStringSubmatch(spec)
	if m == nil {
		return "", ""
	}
	return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
}

// ── uv.lock ───────────────────────────────────────────────────────────────────

type uvLockParser struct{}

func (uvLockParser) Match(path string) bool { return baseName(path) == "uv.lock" }

type uvLockPackage struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
}

type uvLockFile struct {
	Package []uvLockPackage `toml:"package"`
}

func (uvLockParser) Parse(content, filePath string) ([]Package, error) {
	var data uvLockFile
	if _, err := toml.Decode(content, &data); err != nil {
		return nil, err
	}
	var out []Package
	for _, p := range data.Package {
		if p.Name == "" || p.Version == "" {
			continue
		}
		out = append(out, Package{
			Name:     p.Name,
			Kind:     "pip",
			Version:  p.Version,
			FilePath: filePath,
		})
	}
	return out, nil
}

// ── poetry.lock ───────────────────────────────────────────────────────────────

type poetryLockParser struct{}

func (poetryLockParser) Match(path string) bool { return baseName(path) == "poetry.lock" }

// Same [[package]] shape as uv.lock; delegate.
func (poetryLockParser) Parse(content, filePath string) ([]Package, error) {
	return uvLockParser{}.Parse(content, filePath)
}

// ── Pipfile.lock ──────────────────────────────────────────────────────────────
//
// Pipfile.lock is JSON with top-level "default" and "develop" maps of
// name → { "version": "==1.2.3", ... }. `parser` was not in Mira's original
// but is registered here for completeness — it's the standard pipenv lockfile
// shape and downstream vuln matching benefits from resolved versions.

type pipfileLockParser struct{}

func (pipfileLockParser) Match(path string) bool { return baseName(path) == "Pipfile.lock" }

type pipfileLockEntry struct {
	Version string `json:"version"`
}

type pipfileLockFile struct {
	Default map[string]pipfileLockEntry `json:"default"`
	Develop map[string]pipfileLockEntry `json:"develop"`
}

func (pipfileLockParser) Parse(content, filePath string) ([]Package, error) {
	var data pipfileLockFile
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, err
	}
	var out []Package
	for name, entry := range data.Default {
		if entry.Version == "" {
			continue
		}
		out = append(out, Package{
			Name:     name,
			Kind:     "pip",
			Version:  entry.Version,
			FilePath: filePath,
		})
	}
	for name, entry := range data.Develop {
		if entry.Version == "" {
			continue
		}
		out = append(out, Package{
			Name:     name,
			Kind:     "pip",
			Version:  entry.Version,
			FilePath: filePath,
			IsDev:    true,
		})
	}
	return out, nil
}
