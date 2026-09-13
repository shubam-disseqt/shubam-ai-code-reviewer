// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/extract.py under Apache License 2.0.

package extract

import "regexp"

var (
	tsInterface = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?interface\s+(\w+)`)
	tsType      = regexp.MustCompile(`^\s*(?:export\s+)?type\s+(\w+)`)
	tsEnum      = regexp.MustCompile(`^\s*(?:export\s+)?(?:const\s+)?enum\s+(\w+)`)
	// Generic function form: `function name<T>(...)` — captured by jsFunc too,
	// but TS lets you write `function name<T,U>(a: T): U { ... }` which the
	// paren-only signature above already tolerates.
)

// TypeScriptExtractor extends the JS extractor with interface / type / enum.
func TypeScriptExtractor(content, _ string) Result {
	return jsLike(content, true)
}

// tsExtraSymbol matches TS-only declarations. Called per-line from jsLike.
func tsExtraSymbol(line string) (Symbol, bool) {
	if m := tsInterface.FindStringSubmatch(line); m != nil {
		return Symbol{Name: m[1], Kind: "interface", Signature: m[1]}, true
	}
	if m := tsType.FindStringSubmatch(line); m != nil {
		return Symbol{Name: m[1], Kind: "type", Signature: m[1]}, true
	}
	if m := tsEnum.FindStringSubmatch(line); m != nil {
		return Symbol{Name: m[1], Kind: "enum", Signature: m[1]}, true
	}
	return Symbol{}, false
}
