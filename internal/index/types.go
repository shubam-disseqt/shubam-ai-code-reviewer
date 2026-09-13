// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/store.py under Apache License 2.0.

// Package index builds and queries a SQLite-backed per-repo code index.
// Each indexed file gets an LLM-generated summary plus extracted symbols,
// imports, and cross-file references — the raw material for context
// selection during review.
package index

import (
	"time"

	"github.com/shubam-disseqt/z-code-reviewer/internal/filetype"
)

// FileSummary is the top-level record stored per file.
type FileSummary struct {
	Path         string
	Language     filetype.Language
	Summary      string
	ContentHash  string
	LOC          int
	UpdatedAt    time.Time
	Symbols      []SymbolInfo
	Imports      []string      // repo-relative paths
	SymbolRefs   []SymbolRef   // outgoing symbol calls
	ExternalRefs []ExternalRef // non-repo dependencies
}

// SymbolInfo is one exported symbol extracted from a file.
type SymbolInfo struct {
	Name        string
	Kind        string // "function" | "class" | "method" | "constant"
	Signature   string
	Description string
}

// ExternalRef is a reference outside the repo (terraform module, docker
// image, npm/pip package, git URL, remote go import, api endpoint).
type ExternalRef struct {
	Kind        string
	Target      string
	Description string
}

// SymbolRef is a directed edge: SourceSymbol in SourcePath calls
// TargetSymbol in TargetPath.
type SymbolRef struct {
	SourcePath   string
	SourceSymbol string
	TargetPath   string
	TargetSymbol string
}

// DirectorySummary is one directory-level summary row.
type DirectorySummary struct {
	Path      string
	Summary   string
	FileCount int
	UpdatedAt time.Time
}

// PackageManifest is one declared dependency parsed from a manifest file.
type PackageManifest struct {
	Name      string
	Kind      string // "npm" | "pip" | "docker" | "go" | "rust" | "composer" | ...
	Version   string
	FilePath  string
	IsDev     bool
	UpdatedAt time.Time
}

// BlastRadiusEntry describes one file affected by a change to some other
// file's symbols, produced by walking the call graph outward from the
// changed paths. Depth is 1 for direct callers, 2 for callers-of-callers.
type BlastRadiusEntry struct {
	Path            string
	Summary         string
	AffectedSymbols []string
	Depth           int
}
