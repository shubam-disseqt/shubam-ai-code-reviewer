// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/manifests.py under Apache License 2.0.

package manifests

import (
	"encoding/json"
	"strings"
)

// ── composer.json ─────────────────────────────────────────────────────────────

type composerJSONParser struct{}

func (composerJSONParser) Match(path string) bool { return baseName(path) == "composer.json" }

type composerJSON struct {
	Require    map[string]string `json:"require"`
	RequireDev map[string]string `json:"require-dev"`
}

func (composerJSONParser) Parse(content, filePath string) ([]Package, error) {
	var data composerJSON
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, err
	}
	var out []Package
	appendBlock := func(m map[string]string, isDev bool) {
		for name, ver := range m {
			// Skip platform/virtual: php, ext-*, lib-*, hhvm, composer-plugin-api, etc.
			// Real Packagist packages are vendor/package (contain '/').
			if !strings.Contains(name, "/") {
				continue
			}
			out = append(out, Package{
				Name:     name,
				Kind:     "composer",
				Version:  strings.TrimSpace(ver),
				FilePath: filePath,
				IsDev:    isDev,
			})
		}
	}
	appendBlock(data.Require, false)
	appendBlock(data.RequireDev, true)
	return out, nil
}

// ── composer.lock ─────────────────────────────────────────────────────────────

type composerLockParser struct{}

func (composerLockParser) Match(path string) bool { return baseName(path) == "composer.lock" }

type composerLockEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type composerLockFile struct {
	Packages    []composerLockEntry `json:"packages"`
	PackagesDev []composerLockEntry `json:"packages-dev"`
}

func (composerLockParser) Parse(content, filePath string) ([]Package, error) {
	var data composerLockFile
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, err
	}
	var out []Package
	for _, e := range data.Packages {
		if e.Name == "" || e.Version == "" {
			continue
		}
		out = append(out, Package{
			Name:     e.Name,
			Kind:     "composer",
			Version:  e.Version,
			FilePath: filePath,
		})
	}
	for _, e := range data.PackagesDev {
		if e.Name == "" || e.Version == "" {
			continue
		}
		out = append(out, Package{
			Name:     e.Name,
			Kind:     "composer",
			Version:  e.Version,
			FilePath: filePath,
			IsDev:    true,
		})
	}
	return out, nil
}
