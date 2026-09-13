// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/extract.py under Apache License 2.0.

package extract

import "github.com/shubam-disseqt/z-code-reviewer/internal/filetype"

// Registry maps a Language to its Extractor.
type Registry map[filetype.Language]Extractor

// NewRegistry returns the default per-language extractor table.
func NewRegistry() Registry {
	return Registry{
		filetype.LangPython:     PythonExtractor,
		filetype.LangJavaScript: JavaScriptExtractor,
		filetype.LangTypeScript: TypeScriptExtractor,
		filetype.LangRuby:       RubyExtractor,
		filetype.LangGo:         GoExtractor,
		filetype.LangRust:       RustExtractor,
		filetype.LangJava:       JavaExtractor,
		filetype.LangCPP:        CPPExtractor,
		filetype.LangC:          CPPExtractor,
	}
}

// defaultRegistry is used by the top-level Extract dispatcher.
var defaultRegistry = NewRegistry()

// Extract dispatches to the extractor registered for lang. Unknown languages
// return an empty Result.
func Extract(lang filetype.Language, content, path string) Result {
	if fn, ok := defaultRegistry[lang]; ok {
		return fn(content, path)
	}
	return Result{}
}

// dedupe returns xs with consecutive-order-preserved deduplication.
func dedupe(xs []string) []string {
	if len(xs) == 0 {
		return xs
	}
	seen := make(map[string]struct{}, len(xs))
	out := xs[:0]
	for _, x := range xs {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		out = append(out, x)
	}
	return out
}
