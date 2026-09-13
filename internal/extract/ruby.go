// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/extract.py and
// jit_context.py under Apache License 2.0.

package extract

import (
	"regexp"
	"strings"
)

var (
	rbDef     = regexp.MustCompile(`^\s*def\s+(?:self\.)?(\w+)\s*(\([^)]*\))?`)
	rbClass   = regexp.MustCompile(`^\s*class\s+(\w+)`)
	rbModule  = regexp.MustCompile(`^\s*module\s+(\w+)`)
	rbRequire = regexp.MustCompile(`^\s*require(?:_relative)?\s+['"]([^'"]+)['"]`)
)

// RubyExtractor pulls symbols + imports from Ruby source.
func RubyExtractor(content, _ string) Result {
	var res Result
	for _, raw := range strings.Split(content, "\n") {
		line := stripTrailingComment(raw, '#')

		if m := rbDef.FindStringSubmatch(line); m != nil {
			sig := m[1]
			if m[2] != "" {
				sig = m[1] + m[2]
			}
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "function", Signature: sig})
			continue
		}
		if m := rbClass.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "class", Signature: m[1]})
			continue
		}
		if m := rbModule.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "module", Signature: m[1]})
			continue
		}
		if m := rbRequire.FindStringSubmatch(line); m != nil {
			res.Imports = append(res.Imports, m[1])
		}
	}
	res.Imports = dedupe(res.Imports)
	return res
}
