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
	cppClass  = regexp.MustCompile(`^\s*(?:template\s*<[^>]*>\s*)?class\s+(\w+)`)
	cppStruct = regexp.MustCompile(`^\s*(?:template\s*<[^>]*>\s*)?struct\s+(\w+)`)
	// Best-effort function match: `[qualifiers] Type name(...)`. Avoids
	// keywords via a small deny list below.
	cppFunc     = regexp.MustCompile(`^\s*(?:(?:inline|static|virtual|explicit|constexpr|extern|friend)\s+)*(?:\w+(?:::\w+)*(?:\s*<[^>]*>)?[*&\s]+)+(\w+)\s*\([^;]*\)\s*(?:const\s*)?(?:\{|$)`)
	cppInclude  = regexp.MustCompile(`^\s*#\s*include\s*"([^"]+)"`)
	cppSystemRe = regexp.MustCompile(`^\s*#\s*include\s*<([^>]+)>`)
)

// CPPExtractor pulls symbols + imports from C/C++ source.
// Note: `#include <system>` is intentionally skipped per spec.
func CPPExtractor(content, _ string) Result {
	var res Result
	for _, raw := range strings.Split(content, "\n") {
		line := stripLineComment(raw)

		if m := cppClass.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "class", Signature: m[1]})
			continue
		}
		if m := cppStruct.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "struct", Signature: m[1]})
			continue
		}
		if m := cppFunc.FindStringSubmatch(line); m != nil {
			if isCppReservedName(m[1]) {
				continue
			}
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "function", Signature: m[1]})
			continue
		}

		if m := cppInclude.FindStringSubmatch(line); m != nil {
			res.Imports = append(res.Imports, m[1])
			continue
		}
		// Explicitly skip `#include <system>` — do not record.
		_ = cppSystemRe
	}
	res.Imports = dedupe(res.Imports)
	return res
}

func isCppReservedName(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "return", "sizeof", "operator", "new", "delete":
		return true
	}
	return false
}
