// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/manifests.py under Apache License 2.0.

package manifests

import "path/filepath"

// Registry dispatches a path to the first registered parser that matches.
//
// Lockfiles are registered before their manifest counterparts so downstream
// consumers preferring resolved versions can key off ordering. The Registry
// itself does not deduplicate: each parser returns its own view.
type Registry struct {
	parsers []Parser
}

// NewRegistry returns a Registry with the default parser set in the
// documented order (lockfiles first).
func NewRegistry() *Registry {
	return &Registry{
		parsers: []Parser{
			// Lockfiles (resolved versions) first.
			uvLockParser{},
			poetryLockParser{},
			pipfileLockParser{},
			composerLockParser{},
			packageLockJSONParser{},
			pnpmLockParser{},
			yarnLockParser{},
			// Manifests.
			packageJSONParser{},
			requirementsTxtParser{},
			pyprojectTOMLParser{},
			goModParser{},
			composerJSONParser{},
			dockerfileParser{},
		},
	}
}

// Parsers returns the ordered parser slice. Handy for tests.
func (r *Registry) Parsers() []Parser { return r.parsers }

// Match reports whether any registered parser matches the path.
func (r *Registry) Match(path string) bool {
	for _, p := range r.parsers {
		if p.Match(path) {
			return true
		}
	}
	return false
}

// Parse dispatches to the first parser whose Match returns true.
// Returns (nil, nil) when no parser matches. On parser error the error is
// returned so the caller can log; callers who prefer robustness may ignore it.
func (r *Registry) Parse(path, content string) ([]Package, error) {
	for _, p := range r.parsers {
		if p.Match(path) {
			return p.Parse(content, path)
		}
	}
	return nil, nil
}

// baseName returns the final path component using forward-slash-safe logic.
func baseName(path string) string {
	// filepath.Base handles OS separators; also normalize forward slashes.
	return filepath.Base(filepath.ToSlash(path))
}
