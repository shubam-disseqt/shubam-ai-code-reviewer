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

// Symbol-line patterns. Applied AFTER comment/string stripping so they never
// see quoted-string contents. Anchored to the top-level (no leading space) —
// Go top-level declarations always start in column 0.
var (
	goFunc     = regexp.MustCompile(`^func\s+(?:\(([^)]+)\)\s+)?(\w+)\s*(\([^)]*\))`)
	goTypeDecl = regexp.MustCompile(`^type\s+(\w+)\s+(struct|interface)\b`)
	// Type alias or defined type: `type Name = X` or `type Name Underlying`.
	goTypeAlias = regexp.MustCompile(`^type\s+(\w+)\s+`)
	goConstVar  = regexp.MustCompile(`^(const|var)\s+(\w+)`)
	// Single-line import: `import "path"` or `import alias "path"` or
	// `import _ "path"` or `import . "path"`.
	goImportSingle = regexp.MustCompile(`^import\s+(?:([A-Za-z_]\w*|_|\.)\s+)?"([^"]+)"\s*$`)
	// Line inside `import (` block: same shape but no leading `import`.
	goImportBlockLine = regexp.MustCompile(`^\s*(?:([A-Za-z_]\w*|_|\.)\s+)?"([^"]+)"\s*$`)
	// One-line variants of grouped decls: `const ( ... )` / `var ( ... )` /
	// `type ( ... )`. We handle the block state below.
	goGroupHeader = regexp.MustCompile(`^(const|var|type)\s*\(\s*$`)
	// Inside a `const (` or `var (` block: `Name = ...` or `Name Type = ...`.
	goGroupItem = regexp.MustCompile(`^\s*(\w+)\b`)
)

// GoExtractor pulls symbols + imports from Go source.
//
// This is a state machine, NOT a whole-file regex, because Go source is full
// of quoted strings (any of which could look like `import "..."`). We walk
// line by line, tracking:
//
//   - block-comment depth (/* ... */ can span lines)
//   - raw-string depth (backtick can span lines)
//   - import-block state (inside a `import (` group)
//   - grouped-decl state (inside `const (` / `var (` / `type (` blocks)
//
// Anything inside a comment or a string is ignored for both symbols and
// imports.
func GoExtractor(content, _ string) Result {
	var res Result

	inBlockComment := false
	inRawString := false
	inImportBlock := false
	// Group state: kind is "" when not in a group, else "const"/"var"/"type".
	groupKind := ""

	for _, raw := range strings.Split(content, "\n") {
		// Pass 1: strip comments and (line-terminating) strings so the
		// symbol/import regexes see only executable-shape text.
		clean, blockOpen, rawOpen := stripGoNoise(raw, inBlockComment, inRawString)
		inBlockComment = blockOpen
		inRawString = rawOpen

		// Skip blank / whitespace-only lines fast.
		if strings.TrimSpace(clean) == "" {
			// Even a "blank" line matters for group termination: the raw
			// closing paren `)` will appear in `clean` though.
			if inImportBlock || groupKind != "" {
				if strings.Contains(raw, ")") && !inBlockComment && !inRawString {
					inImportBlock = false
					groupKind = ""
				}
			}
			continue
		}

		// Import block state — takes priority over symbol matching.
		if inImportBlock {
			trimmed := strings.TrimSpace(clean)
			if trimmed == ")" || strings.HasPrefix(trimmed, ")") {
				inImportBlock = false
				continue
			}
			if m := goImportBlockLine.FindStringSubmatch(clean); m != nil {
				res.Imports = append(res.Imports, m[2])
			}
			continue
		}

		// Grouped decl state (const/var/type block).
		if groupKind != "" {
			trimmed := strings.TrimSpace(clean)
			if trimmed == ")" || strings.HasPrefix(trimmed, ")") {
				groupKind = ""
				continue
			}
			if m := goGroupItem.FindStringSubmatch(clean); m != nil {
				kind := ""
				switch groupKind {
				case "const":
					kind = "constant"
				case "var":
					kind = "variable"
				case "type":
					kind = "type"
				}
				res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: kind, Signature: m[1]})
			}
			continue
		}

		// Top-level: does this line START an import? Handle single + block.
		if strings.HasPrefix(clean, "import") {
			trimmed := strings.TrimSpace(clean)
			if trimmed == "import (" || strings.HasPrefix(trimmed, "import (") {
				inImportBlock = true
				// Handle rare `import ( "path" )` all-on-one-line form.
				if strings.Contains(trimmed, ")") {
					inner := trimmed[len("import ("):]
					inner = strings.TrimSuffix(strings.TrimSpace(inner), ")")
					for _, part := range strings.Fields(inner) {
						if strings.HasPrefix(part, `"`) && strings.HasSuffix(part, `"`) {
							res.Imports = append(res.Imports, strings.Trim(part, `"`))
						}
					}
					inImportBlock = false
				}
				continue
			}
			if m := goImportSingle.FindStringSubmatch(trimmed); m != nil {
				res.Imports = append(res.Imports, m[2])
				continue
			}
			continue
		}

		// Top-level symbol lines.
		if m := goFunc.FindStringSubmatch(clean); m != nil {
			kind := "function"
			sig := m[2] + m[3]
			if m[1] != "" {
				kind = "method"
				sig = "(" + strings.TrimSpace(m[1]) + ") " + m[2] + m[3]
			}
			res.Symbols = append(res.Symbols, Symbol{Name: m[2], Kind: kind, Signature: sig})
			continue
		}
		if m := goTypeDecl.FindStringSubmatch(clean); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: m[2], Signature: m[1]})
			continue
		}
		if m := goGroupHeader.FindStringSubmatch(strings.TrimSpace(clean)); m != nil {
			groupKind = m[1]
			continue
		}
		if m := goConstVar.FindStringSubmatch(clean); m != nil {
			kind := "variable"
			if m[1] == "const" {
				kind = "constant"
			}
			res.Symbols = append(res.Symbols, Symbol{Name: m[2], Kind: kind, Signature: m[2]})
			continue
		}
		// Type alias / defined type that is neither struct nor interface.
		if m := goTypeAlias.FindStringSubmatch(clean); m != nil {
			res.Symbols = append(res.Symbols, Symbol{Name: m[1], Kind: "type", Signature: m[1]})
			continue
		}
	}

	res.Imports = dedupe(res.Imports)
	return res
}

// stripGoNoise returns line with block comments, line comments, and strings
// replaced by spaces (keeping column indices stable). It also reports whether
// the returned state leaves a block comment or raw string still open.
//
// This is deliberately not a full lexer — it only needs to be good enough
// that quoted content doesn't leak into the symbol/import regex layer.
func stripGoNoise(line string, inBlock, inRaw bool) (string, bool, bool) {
	b := []byte(line)
	out := make([]byte, len(b))
	i := 0
	for i < len(b) {
		c := b[i]

		// Continue an open block comment.
		if inBlock {
			out[i] = ' '
			if c == '*' && i+1 < len(b) && b[i+1] == '/' {
				out[i+1] = ' '
				i += 2
				inBlock = false
				continue
			}
			i++
			continue
		}
		// Continue an open raw string.
		if inRaw {
			out[i] = ' '
			if c == '`' {
				inRaw = false
			}
			i++
			continue
		}

		// Start of a comment?
		if c == '/' && i+1 < len(b) {
			if b[i+1] == '/' {
				// Line comment — blank out the rest.
				for j := i; j < len(b); j++ {
					out[j] = ' '
				}
				return string(out), inBlock, inRaw
			}
			if b[i+1] == '*' {
				out[i] = ' '
				out[i+1] = ' '
				i += 2
				inBlock = true
				continue
			}
		}

		// Start of a raw string?
		if c == '`' {
			out[i] = ' '
			i++
			inRaw = true
			continue
		}

		// Start of a regular string. Copy bytes verbatim: the import regex
		// wants to see quoted paths on lines that are actually imports; and
		// non-import lines with strings won't match `^import`, `^func`, etc.
		if c == '"' {
			out[i] = c
			i++
			for i < len(b) {
				out[i] = b[i]
				if b[i] == '\\' && i+1 < len(b) {
					out[i+1] = b[i+1]
					i += 2
					continue
				}
				if b[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}

		// Single-quoted rune literal.
		if c == '\'' {
			out[i] = c
			i++
			for i < len(b) {
				out[i] = b[i]
				if b[i] == '\\' && i+1 < len(b) {
					out[i+1] = b[i+1]
					i += 2
					continue
				}
				if b[i] == '\'' {
					i++
					break
				}
				i++
			}
			continue
		}

		out[i] = c
		i++
	}
	return string(out), inBlock, inRaw
}
