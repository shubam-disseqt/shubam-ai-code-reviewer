// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/extract.py under Apache License 2.0.

package extract

import (
	"regexp"
	"strings"
)

// Python patterns. Mira uses an indent walker for scoping; we keep the
// pattern-match half but do not bother tracking scope for symbol emission —
// per PORTING.md §7 "being more precise CHANGES the JIT context quality".
var (
	pyDef     = regexp.MustCompile(`^(\s*)(async\s+)?def\s+(\w+)\s*(\([^)]*\))?`)
	pyClass   = regexp.MustCompile(`^(\s*)class\s+(\w+)`)
	pyImport  = regexp.MustCompile(`^\s*import\s+([\w.]+(?:\s*,\s*[\w.]+)*)`)
	pyFromImp = regexp.MustCompile(`^\s*from\s+([\w.]+)\s+import\s+(.+)$`)
)

// PythonExtractor pulls symbols + imports from Python source.
func PythonExtractor(content, _ string) Result {
	var res Result
	for _, raw := range strings.Split(content, "\n") {
		line := stripTrailingComment(raw, '#')

		if m := pyDef.FindStringSubmatch(line); m != nil {
			indent := len(m[1])
			kind := "function"
			if indent > 0 {
				kind = "method"
			}
			sig := m[3]
			if m[4] != "" {
				sig = m[3] + m[4]
			}
			res.Symbols = append(res.Symbols, Symbol{
				Name:      m[3],
				Kind:      kind,
				Signature: sig,
			})
			continue
		}
		if m := pyClass.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{
				Name:      m[2],
				Kind:      "class",
				Signature: m[2],
			})
			continue
		}

		if m := pyFromImp.FindStringSubmatch(line); m != nil {
			// `from X import Y, Z` — record X plus each imported name as X.Y.
			base := m[1]
			for _, name := range splitImportNames(m[2]) {
				if name == "" {
					continue
				}
				res.Imports = append(res.Imports, base+"."+name)
			}
			// Also record the base module so callers can resolve either shape.
			res.Imports = append(res.Imports, base)
			continue
		}
		if m := pyImport.FindStringSubmatch(line); m != nil {
			for _, name := range splitImportNames(m[1]) {
				if name != "" {
					res.Imports = append(res.Imports, name)
				}
			}
		}
	}
	res.Imports = dedupe(res.Imports)
	return res
}

// splitImportNames splits an `import a, b as c, d` clause on commas and drops
// any `as <alias>` tail, returning the imported names.
func splitImportNames(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if idx := strings.Index(p, " as "); idx >= 0 {
			p = strings.TrimSpace(p[:idx])
		}
		// Drop parens from `from X import (a, b)` forms.
		p = strings.Trim(p, "()")
		p = strings.TrimSpace(p)
		if p == "*" || p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// stripTrailingComment removes a trailing `# comment` (or `// comment` for
// C-like) from line, respecting no string context — best-effort like the rest
// of this package.
func stripTrailingComment(line string, marker byte) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line):
			i++
		case !inDouble && c == '\'':
			inSingle = !inSingle
		case !inSingle && c == '"':
			inDouble = !inDouble
		case !inSingle && !inDouble && c == marker:
			return line[:i]
		}
	}
	return line
}
