// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/extract.py under Apache License 2.0.

package extract

import (
	"regexp"
	"strings"
)

var (
	rustFn     = regexp.MustCompile(`^(\s*)(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:unsafe\s+)?(?:extern\s+"[^"]+"\s+)?fn\s+(\w+)\s*(?:<[^>]*>)?\s*(\([^)]*\))?`)
	rustStruct = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?struct\s+(\w+)`)
	rustEnum   = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?enum\s+(\w+)`)
	rustTrait  = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:unsafe\s+)?trait\s+(\w+)`)
	rustImpl   = regexp.MustCompile(`^\s*impl(?:<[^>]*>)?\s+(?:.+\s+for\s+)?(\w+)`)
	rustUse    = regexp.MustCompile(`^\s*(?:pub\s+)?use\s+([^;]+);`)
)

// RustExtractor pulls symbols + imports from Rust source.
func RustExtractor(content, _ string) Result {
	var res Result
	for _, raw := range strings.Split(content, "\n") {
		line := stripLineComment(raw)

		if m := rustFn.FindStringSubmatch(line); m != nil {
			sig := m[2]
			if m[3] != "" {
				sig = m[2] + m[3]
			}
			res.Symbols = append(res.Symbols, Symbol{Name: m[2], Kind: "function", Signature: sig})
			continue
		}
		if m := rustStruct.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "struct", Signature: m[1]})
			continue
		}
		if m := rustEnum.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "enum", Signature: m[1]})
			continue
		}
		if m := rustTrait.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "trait", Signature: m[1]})
			continue
		}
		if m := rustImpl.FindStringSubmatch(line); m != nil {
			// impl blocks expose the target type as a symbol; kind stays "type".
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "type", Signature: m[1]})
			continue
		}

		if m := rustUse.FindStringSubmatch(line); m != nil {
			for _, p := range expandRustUse(strings.TrimSpace(m[1])) {
				if p != "" {
					res.Imports = append(res.Imports, p)
				}
			}
		}
	}
	res.Imports = dedupe(res.Imports)
	return res
}

// expandRustUse expands a use-tree like `a::b::{c, d::{e, f}}` into a flat
// list of paths: `a::b::c`, `a::b::d::e`, `a::b::d::f`. Best-effort; a
// pathological deeply-nested tree still resolves but with a small stack.
func expandRustUse(clause string) []string {
	// Fast path: no braces.
	if !strings.Contains(clause, "{") {
		clause = strings.TrimSpace(clause)
		if strings.HasSuffix(clause, "::*") {
			clause = strings.TrimSuffix(clause, "::*")
		}
		if idx := strings.Index(clause, " as "); idx >= 0 {
			clause = strings.TrimSpace(clause[:idx])
		}
		if clause == "" {
			return nil
		}
		return []string{clause}
	}

	openIdx := strings.Index(clause, "{")
	prefix := strings.TrimSuffix(strings.TrimSpace(clause[:openIdx]), "::")
	// Find matching '}'.
	depth := 0
	closeIdx := -1
	for i := openIdx; i < len(clause); i++ {
		switch clause[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closeIdx = i
			}
		}
		if closeIdx >= 0 {
			break
		}
	}
	if closeIdx < 0 {
		return []string{prefix}
	}
	inner := clause[openIdx+1 : closeIdx]

	parts := splitTopLevelCommas(inner)
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "self" {
			if prefix != "" {
				out = append(out, prefix)
			}
			continue
		}
		joined := part
		if prefix != "" {
			joined = prefix + "::" + part
		}
		out = append(out, expandRustUse(joined)...)
	}
	return out
}

// splitTopLevelCommas splits on commas at brace depth 0.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}
