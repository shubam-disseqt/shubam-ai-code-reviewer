// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/extract.py under Apache License 2.0.

// Package extract provides best-effort per-language symbol and import extraction.
// It intentionally mirrors Mira's regex-heuristic approach: precision is not the
// goal, the downstream blast-radius / JIT-context queries tolerate junk.
package extract

// Symbol is a named declaration lifted from a source file. Kind is one of:
// "function", "method", "class", "struct", "interface", "type", "enum",
// "constant", "variable", "module", "trait".
type Symbol struct {
	Name        string
	Kind        string
	Signature   string
	Description string
}

// Result holds every symbol and import path recovered from a single file.
// Imports are the raw string as it appears in the source (bare package name,
// dotted module, relative path, etc.) — resolution is the caller's job.
type Result struct {
	Symbols []Symbol
	Imports []string
}

// Extractor is a per-language function that turns file content into a Result.
// path is passed for context (extension hints, error reporting) but most
// extractors ignore it.
type Extractor func(content string, path string) Result
