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
	jsFunc = regexp.MustCompile(`^(\s*)(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+(\w+)\s*(\([^)]*\))?`)
	// Arrow-function assigned to const/let/var: allow multi-arg, async, etc.
	jsArrow = regexp.MustCompile(`^(\s*)(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:\([^)]*\)|\w+)\s*=>`)
	// Plain const/let/var (no arrow): variable/constant declaration.
	jsVar   = regexp.MustCompile(`^(\s*)(?:export\s+)?(const|let|var)\s+(\w+)\s*=`)
	jsClass = regexp.MustCompile(`^(\s*)(?:export\s+)?(?:default\s+)?class\s+(\w+)`)

	// ES6: `import ... from 'path'`  and side-effect `import 'path'`.
	jsImport = regexp.MustCompile(`(?m)^\s*import\b[^'"\n]*['"]([^'"]+)['"]`)
	// CommonJS: `require('path')`.
	jsRequire = regexp.MustCompile(`require\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	// Dynamic import: `import('path')`.
	jsDynImport = regexp.MustCompile(`import\s*\(\s*['"]([^'"]+)['"]\s*\)`)
)

// JavaScriptExtractor pulls symbols + imports from JS source.
func JavaScriptExtractor(content, _ string) Result {
	return jsLike(content, false)
}

func jsLike(content string, ts bool) Result {
	var res Result
	for _, raw := range strings.Split(content, "\n") {
		line := stripLineComment(raw)
		// For import scans we also need string-content blanked so a
		// `const s = "require('fake')"` doesn't leak.
		lineNoStr := blankStrings(line)

		if m := jsClass.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[2], Kind: "class", Signature: m[2]})
			continue
		}
		if m := jsFunc.FindStringSubmatch(line); m != nil {
			sig := m[2]
			if m[3] != "" {
				sig = m[2] + m[3]
			}
			res.Symbols = append(res.Symbols, Symbol{Name: m[2], Kind: "function", Signature: sig})
			continue
		}
		if m := jsArrow.FindStringSubmatch(line); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[2], Kind: "function", Signature: m[2]})
			continue
		}
		if m := jsVar.FindStringSubmatch(line); m != nil {
			kind := "variable"
			if m[2] == "const" {
				kind = "constant"
			}
			res.Symbols = append(res.Symbols, Symbol{Name: m[3], Kind: kind, Signature: m[3]})
			// fall through — const/let/var may still contain require(...).
		}

		if ts {
			if s, ok := tsExtraSymbol(line); ok {
				res.Symbols = append(res.Symbols, s)
			}
		}

		// Imports: only trust a line whose non-string skeleton mentions
		// `import` or `require`. Then re-run the regex against the ORIGINAL
		// line so the quoted path itself is captured.
		if strings.Contains(lineNoStr, "import") {
			if m := jsImport.FindStringSubmatch(line); m != nil {
				res.Imports = append(res.Imports, m[1])
			}
			if m := jsDynImport.FindStringSubmatch(line); m != nil {
				res.Imports = append(res.Imports, m[1])
			}
		}
		if strings.Contains(lineNoStr, "require") {
			if m := jsRequire.FindStringSubmatch(line); m != nil {
				res.Imports = append(res.Imports, m[1])
			}
		}
	}
	res.Imports = dedupe(res.Imports)
	return res
}

// blankStrings replaces the contents of every '...', "..." and `...` literal
// on a single line with spaces. Multi-line template literals leak, but the
// downstream check just needs to distinguish "this line's `require` is code"
// from "this line's `require` is inside a string literal".
func blankStrings(line string) string {
	b := []byte(line)
	out := make([]byte, len(b))
	inSingle, inDouble, inTick := false, false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c == '\\' && i+1 < len(b):
			if inSingle || inDouble || inTick {
				out[i] = ' '
				out[i+1] = ' '
			} else {
				out[i] = c
				out[i+1] = b[i+1]
			}
			i++
		case !inDouble && !inTick && c == '\'':
			out[i] = ' '
			inSingle = !inSingle
		case !inSingle && !inTick && c == '"':
			out[i] = ' '
			inDouble = !inDouble
		case !inSingle && !inDouble && c == '`':
			out[i] = ' '
			inTick = !inTick
		case inSingle || inDouble || inTick:
			out[i] = ' '
		default:
			out[i] = c
		}
	}
	return string(out)
}

// stripLineComment removes a `// ...` tail from line, respecting strings.
func stripLineComment(line string) string {
	inSingle, inDouble, inTick := false, false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line):
			i++
		case !inDouble && !inTick && c == '\'':
			inSingle = !inSingle
		case !inSingle && !inTick && c == '"':
			inDouble = !inDouble
		case !inSingle && !inDouble && c == '`':
			inTick = !inTick
		case !inSingle && !inDouble && !inTick && c == '/' && i+1 < len(line) && line[i+1] == '/':
			return line[:i]
		}
	}
	return line
}
