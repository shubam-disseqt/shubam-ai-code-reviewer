// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package depgraph

import (
	"path"
	"regexp"
	"strings"
)

// tsStaticImportRe matches statement-level import/export shapes anchored
// to line start so we don't catch the word "import" inside a string.
//   - import ... from 'X'   (static, named/default/namespace/aliased)
//   - import 'X'            (side-effect-only)
//   - export ... from 'X'   (re-export, including `export * from`)
var tsStaticImportRe = regexp.MustCompile(
	`(?m)^\s*(?:import\s+[^'";]*from\s*['"]([^'"]+)['"]` +
		`|import\s*['"]([^'"]+)['"]` +
		`|export\s+[^'";]*from\s*['"]([^'"]+)['"])`,
)

// tsDynamicImportRe matches `import('X')` anywhere in the file — dynamic
// imports are expressions, not statements, so they can appear mid-line
// (e.g. `const m = await import('./mod')`).
var tsDynamicImportRe = regexp.MustCompile(
	`\bimport\s*\(\s*['"]([^'"]+)['"]\s*\)`,
)

// extractTypeScript scans a TS/TSX/MTS/CTS file for import specifiers and
// records project-local edges into seen. Relative specifiers are resolved
// against the source file's directory; bare specifiers (react, lodash,
// node_modules) are treated as external and dropped unless
// IncludeExternal is true.
func extractTypeScript(f File, opts Options, seen map[edge]struct{}) {
	srcPkg := shortPackage(path.Dir(f.Path), "")
	if srcPkg == "" {
		return
	}
	record := func(spec string) {
		if spec == "" {
			return
		}
		dst := resolveTSSpec(spec, path.Dir(f.Path), opts.ModulePrefix)
		if dst == "" {
			if !opts.IncludeExternal {
				return
			}
			dst = spec
		}
		if dst == "" || dst == srcPkg {
			return
		}
		seen[edge{Src: srcPkg, Dst: dst}] = struct{}{}
	}
	for _, m := range tsStaticImportRe.FindAllSubmatch(f.Content, -1) {
		record(firstNonEmpty(m[1:]))
	}
	for _, m := range tsDynamicImportRe.FindAllSubmatch(f.Content, -1) {
		record(string(m[1]))
	}
}

// resolveTSSpec turns an import specifier into a repo-relative package
// label, or returns "" for external/bare specifiers. Relative paths
// (./x, ../y) resolve against srcDir; ModulePrefix-prefixed specifiers
// (e.g. "@app/foo" when ModulePrefix="@app/") strip the prefix.
func resolveTSSpec(spec, srcDir, modulePrefix string) string {
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
		resolved := path.Clean(path.Join(srcDir, spec))
		return trimTSFileExt(resolved)
	}
	if modulePrefix != "" && strings.HasPrefix(spec, modulePrefix) {
		return trimTSFileExt(strings.TrimPrefix(spec, modulePrefix))
	}
	return ""
}

// trimTSFileExt drops a trailing TS-flavored extension so the label
// aligns with the directory-style labels the Go extractor produces.
func trimTSFileExt(p string) string {
	for _, ext := range []string{".tsx", ".ts", ".mts", ".cts", ".jsx", ".js"} {
		if strings.HasSuffix(p, ext) {
			return strings.TrimSuffix(p, ext)
		}
	}
	return p
}

// firstNonEmpty returns the first non-empty submatch as a string.
func firstNonEmpty(groups [][]byte) string {
	for _, g := range groups {
		if len(g) > 0 {
			return string(g)
		}
	}
	return ""
}
