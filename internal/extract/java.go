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
	javaClass = regexp.MustCompile(
		`^(\s*)(?:(?:public|private|protected|static|abstract|final|sealed|non-sealed|strictfp)\s+)*` +
			`(class|interface|enum|record)\s+(\w+)`)
	// Method: return type + name + '('.  Best-effort; skips ctors.
	javaMethod = regexp.MustCompile(
		`^(\s*)(?:(?:public|private|protected|static|final|synchronized|abstract|native|default)\s+)*` +
			`(?:\w+(?:<[^>]*>)?(?:\[\])*)\s+(\w+)\s*\(`)
	// `import a.b.c.D;` or `import static a.b.c.D.method;` — wildcard rejected.
	javaImport = regexp.MustCompile(`^\s*import\s+(?:static\s+)?([\w.]+)\s*;`)
)

// JavaExtractor pulls symbols + imports from Java source.
func JavaExtractor(content, _ string) Result {
	var res Result
	for _, raw := range strings.Split(content, "\n") {
		line := stripLineComment(raw)

		if m := javaClass.FindStringSubmatch(line); m != nil {
			kind := m[2]
			if kind == "record" {
				kind = "class"
			}
			res.Symbols = append(res.Symbols, Symbol{Name: m[3], Kind: kind, Signature: m[3]})
			continue
		}
		if m := javaMethod.FindStringSubmatch(line); m != nil {
			// Filter out obvious statements like `if(...)`, `while(...)`, etc.
			// The regex requires a return type before the name so most keywords
			// won't match, but be conservative on reserved words.
			if isJavaReservedName(m[2]) {
				continue
			}
			res.Symbols = append(res.Symbols, Symbol{Name: m[2], Kind: "method", Signature: m[2]})
			continue
		}
		if m := javaImport.FindStringSubmatch(line); m != nil {
			path := m[1]
			// Reject wildcard imports; the regex already excludes `.*`, but be
			// explicit.
			if strings.HasSuffix(path, ".*") || path == "*" {
				continue
			}
			res.Imports = append(res.Imports, path)
		}
	}
	res.Imports = dedupe(res.Imports)
	return res
}

func isJavaReservedName(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "return", "catch", "synchronized", "throw", "new":
		return true
	}
	return false
}
