// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/manifests.py under Apache License 2.0.

// Package manifests contains deterministic parsers for common package
// manifest and lockfile formats (npm, go, pip, docker, composer).
//
// Each parser returns a slice of Package entries. A Registry dispatches by
// path basename to the correct parser. Lockfiles are registered before their
// manifest counterparts so that when both exist, callers preferring resolved
// versions can consult the lockfile output first.
package manifests

// Package is a single dependency declared in a manifest or lockfile.
type Package struct {
	Name     string
	Kind     string // "npm" | "go" | "pip" | "docker" | "composer"
	Version  string // resolved version, or constraint if unresolved
	FilePath string // manifest file that declared it
	IsDev    bool   // dev dependency (npm devDependencies, composer require-dev, etc.)
}

// Parser matches manifest paths and parses their content into Package entries.
type Parser interface {
	// Match reports whether this parser handles the given path.
	Match(path string) bool
	// Parse returns the packages declared in content. On malformed input a
	// parser SHOULD return an error rather than panic; the Registry logs
	// and returns nil to keep indexing robust.
	Parse(content string, filePath string) ([]Package, error)
}
