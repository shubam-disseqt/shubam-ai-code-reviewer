// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/jit_context.py under
// Apache License 2.0.

package reviewctx

import (
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/filetype"
)

// maxCandidatesPerImport caps how many files we accept per raw import. Mira
// keeps this small because heuristic resolvers (Java, go.mod-less Go) can
// otherwise pollute the prompt with off-topic symbols.
const maxCandidatesPerImport = 1

// Regexes ported literally from Mira. Names match the Python constants.
var (
	pyImportRe = regexp.MustCompile(`(?m)^\s*(?:from\s+([\w.]+)\s+import|import\s+([\w.]+))`)
	// JS/TS import strings: `from '...'`, `require('...')`, `import '...'`.
	// Mira only matches from/require, but naked side-effect imports
	// (`import './foo'`) show up often enough that we cover them too.
	jsImportRe    = regexp.MustCompile(`(?:from|require\s*\(|import)\s*['"]([^'"]+)['"]`)
	rubyRequireRe = regexp.MustCompile(`(?m)^\s*require(?:_relative)?\s+['"]([^'"]+)['"]`)
	javaImportRe  = regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([\w.]+(?:\.\*)?)\s*;`)
	// Go imports come in two shapes — see extractGoImportPaths below for why
	// we don't use a whole-file regex.
	goSingleImportRe = regexp.MustCompile(`^\s*import\s+(?:(?:[A-Za-z_]\w*|\.)\s+)?"([^"]+)"\s*$`)
	goBlockPathRe    = regexp.MustCompile(`^\s*(?:(?:[A-Za-z_]\w*|\.)\s+)?"([^"]+)"\s*$`)
	goModuleRe       = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	// Rust: `use crate::foo::bar;`, `use a::b::c;`, `use a::b::{c, d};`
	rustUseRe = regexp.MustCompile(`(?m)^\s*(?:pub\s+)?use\s+([\w:]+)`)
	// C/C++: `#include "foo/bar.hpp"` (angle-bracket forms are system
	// headers — no local file to resolve).
	cxxIncludeRe = regexp.MustCompile(`(?m)^\s*#\s*include\s+"([^"]+)"`)
)

// goStdlibHints is Mira's cheap heuristic for skipping stdlib import paths.
var goStdlibHints = []string{
	"net/", "encoding/", "crypto/", "io/", "os/", "path/", "text/", "html/",
	"log/", "regexp/", "compress/", "container/", "database/", "debug/",
	"go/", "image/", "math/", "mime/", "runtime/", "sync/", "syscall/",
	"testing/", "unicode/", "archive/", "hash/", "index/", "reflect/",
	"sort/", "strconv/", "time/",
}

// ExtractImportCandidates returns candidate repo-relative paths for imports
// found in source. Filtering against a repo tree is the caller's job — this
// only turns raw imports into "here's what the file *might* be".
//
// language is the source-file language. sourcePath is the file's repo-relative
// path (used to resolve relative imports). repoTree is optional; Java and
// (module-less) Go need it — nil means "skip those languages".
// enableJavaGo lets callers disable the heuristic resolvers wholesale.
// goModule is the parsed go.mod `module` line; empty means fall back to
// tail-matching.
func ExtractImportCandidates(
	source string,
	language filetype.Language,
	sourcePath string,
	repoTree map[string]struct{},
	enableJavaGo bool,
	goModule string,
) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 8)
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	switch language {
	case filetype.LangPython:
		for _, m := range pyImportRe.FindAllStringSubmatch(source, -1) {
			mod := m[1]
			if mod == "" {
				mod = m[2]
			}
			for _, c := range candidatesPython(mod, sourcePath) {
				add(c)
			}
		}
	case filetype.LangJavaScript, filetype.LangTypeScript:
		for _, m := range jsImportRe.FindAllStringSubmatch(source, -1) {
			for _, c := range candidatesJS(m[1], sourcePath) {
				add(c)
			}
		}
	case filetype.LangRuby:
		for _, m := range rubyRequireRe.FindAllStringSubmatch(source, -1) {
			for _, c := range candidatesRuby(m[1], sourcePath) {
				add(c)
			}
		}
	case filetype.LangJava:
		if !enableJavaGo {
			return nil
		}
		for _, m := range javaImportRe.FindAllStringSubmatch(source, -1) {
			for _, c := range candidatesJava(m[1], repoTree) {
				add(c)
			}
		}
	case filetype.LangGo:
		if !enableJavaGo {
			return nil
		}
		for _, p := range extractGoImportPaths(source) {
			for _, c := range candidatesGo(p, repoTree, goModule) {
				add(c)
			}
		}
	case filetype.LangRust:
		for _, m := range rustUseRe.FindAllStringSubmatch(source, -1) {
			for _, c := range candidatesRust(m[1], sourcePath, repoTree) {
				add(c)
			}
		}
	case filetype.LangCPP, filetype.LangC:
		for _, m := range cxxIncludeRe.FindAllStringSubmatch(source, -1) {
			for _, c := range candidatesCXX(m[1], sourcePath, repoTree) {
				add(c)
			}
		}
	}
	return out
}

// candidatesPython resolves a dotted module to file-path guesses.
// Mirrors Mira _candidates_python.
func candidatesPython(module, sourcePath string) []string {
	// Skip empty or dot-only modules (`from . import x` — relative to
	// package, no module name to resolve to a file).
	if strings.Trim(module, ".") == "" {
		return nil
	}
	rel := strings.ReplaceAll(module, ".", "/")
	out := []string{rel + ".py", rel + "/__init__.py"}
	srcDir := path.Dir(sourcePath)
	if srcDir != "." && srcDir != "" {
		out = append(out, srcDir+"/"+rel+".py", srcDir+"/"+rel+"/__init__.py")
	}
	for _, root := range []string{"src", "lib"} {
		out = append(out, root+"/"+rel+".py", root+"/"+rel+"/__init__.py")
	}
	return out
}

// candidatesJS handles only relative imports; bare specifiers are npm.
// Mirrors Mira _candidates_js.
func candidatesJS(imp, sourcePath string) []string {
	if imp == "" || !strings.HasPrefix(imp, ".") {
		return nil
	}
	srcDir := path.Dir(sourcePath)
	base := normalizeRelative(srcDir, imp)
	if ext := path.Ext(base); ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx" || ext == ".mjs" {
		return []string{base}
	}
	out := make([]string, 0, 9)
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs"} {
		out = append(out, base+ext)
	}
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
		out = append(out, base+"/index"+ext)
	}
	return out
}

// candidatesRuby handles `require` / `require_relative`. Mirrors _candidates_ruby.
func candidatesRuby(imp, sourcePath string) []string {
	if imp == "" {
		return nil
	}
	srcDir := path.Dir(sourcePath)
	base := normalizeRelative(srcDir, imp)
	if strings.HasSuffix(base, ".rb") {
		return []string{base}
	}
	return []string{base + ".rb"}
}

// candidatesJava resolves a Java FQN by asking the repo tree for any file
// ending in `/<ClassName>.java`. Mirrors _candidates_java.
func candidatesJava(fqn string, repoTree map[string]struct{}) []string {
	if fqn == "" || strings.HasSuffix(fqn, ".*") || repoTree == nil {
		return nil
	}
	parts := strings.Split(fqn, ".")
	// `import static com.foo.Bar.method` — pop trailing lowercase names so
	// we resolve the enclosing class, not the field.
	for len(parts) > 0 && parts[len(parts)-1] != "" && unicode.IsLower(rune(parts[len(parts)-1][0])) {
		parts = parts[:len(parts)-1]
	}
	if len(parts) < 2 {
		return nil
	}
	className := parts[len(parts)-1]
	if className == "" || !unicode.IsUpper(rune(className[0])) {
		return nil
	}
	suffix := "/" + className + ".java"
	standalone := className + ".java"
	var matches []string
	for p := range repoTree {
		if strings.HasSuffix(p, suffix) || p == standalone {
			matches = append(matches, p)
		}
	}
	// Sort: more package-segment overlap first, then shorter paths.
	pkg := strings.Join(parts[:len(parts)-1], ".")
	sort.SliceStable(matches, func(i, j int) bool {
		oi, oj := pathOverlap(matches[i], pkg), pathOverlap(matches[j], pkg)
		if oi != oj {
			return oi > oj
		}
		return len(matches[i]) < len(matches[j])
	})
	if len(matches) > maxCandidatesPerImport {
		matches = matches[:maxCandidatesPerImport]
	}
	return matches
}

// pathOverlap counts, in order, how many segments of pkg (dot-separated)
// appear in filePath (slash-separated).
func pathOverlap(filePath, pkg string) int {
	if pkg == "" {
		return 0
	}
	fpParts := strings.Split(filePath, "/")
	pkgParts := strings.Split(pkg, ".")
	if len(pkgParts) == 0 {
		return 0
	}
	score, j := 0, 0
	for _, seg := range fpParts {
		if j < len(pkgParts) && seg == pkgParts[j] {
			score++
			j++
		}
	}
	return score
}

// ParseGoModule extracts the `module` path from a go.mod source string.
// Empty string on no match.
func ParseGoModule(goModSource string) string {
	m := goModuleRe.FindStringSubmatch(goModSource)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// candidatesGo resolves a Go import path. Behaviour matches Mira _candidates_go:
// with module -> deterministic prefix strip; without -> tail-match.
func candidatesGo(imp string, repoTree map[string]struct{}, module string) []string {
	if imp == "" || repoTree == nil {
		return nil
	}
	p := strings.Trim(strings.TrimSpace(imp), `"`)
	p = strings.TrimSpace(p)
	if p == "" || strings.HasPrefix(p, ".") {
		return nil
	}
	if !strings.Contains(p, "/") {
		return nil // stdlib single-segment (`strings`, `fmt`)
	}
	for _, prefix := range goStdlibHints {
		if strings.HasPrefix(p, prefix) {
			return nil
		}
	}
	segs := strings.Split(p, "/")

	if module != "" {
		if p != module && !strings.HasPrefix(p, module+"/") {
			return nil
		}
		relDir := strings.Trim(p[len(module):], "/")
		prefix := ""
		if relDir != "" {
			prefix = relDir + "/"
		}
		var files []string
		for f := range repoTree {
			if !strings.HasPrefix(f, prefix) {
				continue
			}
			tail := f[len(prefix):]
			if strings.Contains(tail, "/") {
				continue
			}
			if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
				continue
			}
			files = append(files, f)
		}
		// Package-named file first, then alphabetical.
		primary := prefix + segs[len(segs)-1] + ".go"
		sort.SliceStable(files, func(i, j int) bool {
			pi, pj := files[i] == primary, files[j] == primary
			if pi != pj {
				return pi
			}
			return files[i] < files[j]
		})
		if len(files) > 2 {
			files = files[:2]
		}
		return files
	}

	// No module: try progressively shorter tail suffixes.
	for _, tailLen := range []int{3, 2} {
		if tailLen > len(segs) {
			continue
		}
		suffix := strings.Join(segs[len(segs)-tailLen:], "/")
		matchDir := "/" + suffix + "/"
		var files []string
		for f := range repoTree {
			slashed := "/" + f
			if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
				continue
			}
			if strings.Contains(slashed, matchDir) || strings.HasSuffix(slashed, "/"+suffix) {
				files = append(files, f)
			}
		}
		if len(files) > 0 {
			sort.SliceStable(files, func(i, j int) bool { return len(files[i]) < len(files[j]) })
			if len(files) > maxCandidatesPerImport {
				files = files[:maxCandidatesPerImport]
			}
			return files
		}
	}
	return nil
}

// extractGoImportPaths walks Go source line-by-line to find import strings.
// A regex over the whole file catches every quoted string in Go — struct
// tags, format strings, error messages. This state machine only treats
// quoted strings as imports when inside an `import(...)` block or on a
// single-line `import "..."`.
func extractGoImportPaths(source string) []string {
	var paths []string
	inBlock := false
	for _, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "import") {
			// Multi-line block opens on `import (` (no close paren on same line).
			if strings.Contains(line, "(") && !strings.Contains(line, ")") {
				inBlock = true
				continue
			}
			if m := goSingleImportRe.FindStringSubmatch(raw); m != nil {
				paths = append(paths, m[1])
			}
			continue
		}
		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			if m := goBlockPathRe.FindStringSubmatch(raw); m != nil {
				paths = append(paths, m[1])
			}
		}
	}
	return paths
}

// candidatesRust resolves `use a::b::c` to file guesses. Cargo's default
// layout is src/-rooted, so if the source itself lives under src/ we prefer
// that root. `crate::` and `self::` are relative to the crate root; `super::`
// walks up one module.
func candidatesRust(useExpr, sourcePath string, repoTree map[string]struct{}) []string {
	if useExpr == "" {
		return nil
	}
	segs := strings.Split(useExpr, "::")
	// Drop the trailing symbol name (typically capitalized or `*`) — it
	// isn't part of the module path. Keep it when the whole use is a single
	// segment (module import).
	if len(segs) > 1 {
		last := segs[len(segs)-1]
		if last == "" || last == "*" || (len(last) > 0 && unicode.IsUpper(rune(last[0]))) {
			segs = segs[:len(segs)-1]
		}
	}
	if len(segs) == 0 {
		return nil
	}
	// Skip external crates: any first segment we can't map to a folder.
	first := segs[0]
	root := ""
	switch first {
	case "crate", "self":
		root = crateSrcRoot(sourcePath)
		segs = segs[1:]
	case "super":
		root = path.Dir(sourcePath)
		if root == "." {
			root = ""
		}
		segs = segs[1:]
	case "std", "core", "alloc":
		return nil
	default:
		root = ""
	}
	if len(segs) == 0 {
		return nil
	}
	rel := strings.Join(segs, "/")
	guesses := []string{rel + ".rs", rel + "/mod.rs"}
	if root != "" {
		guesses = append(guesses, root+"/"+rel+".rs", root+"/"+rel+"/mod.rs")
	}
	// Also try src/ prefix — the common Cargo layout.
	guesses = append(guesses, "src/"+rel+".rs", "src/"+rel+"/mod.rs")
	if repoTree == nil {
		return guesses
	}
	var kept []string
	for _, g := range guesses {
		if _, ok := repoTree[g]; ok {
			kept = append(kept, g)
		}
	}
	return kept
}

// crateSrcRoot walks up from sourcePath to find a `src/` directory. Empty
// if not under src/.
func crateSrcRoot(sourcePath string) string {
	parts := strings.Split(sourcePath, "/")
	for i, seg := range parts {
		if seg == "src" {
			return strings.Join(parts[:i+1], "/")
		}
	}
	return ""
}

// candidatesCXX resolves `#include "..."`. The literal path may be relative
// to the including file OR to an include root — we try both.
func candidatesCXX(inc, sourcePath string, repoTree map[string]struct{}) []string {
	if inc == "" {
		return nil
	}
	guesses := []string{inc}
	srcDir := path.Dir(sourcePath)
	if srcDir != "." && srcDir != "" {
		guesses = append(guesses, normalizeRelative(srcDir, inc))
	}
	// Common include roots.
	for _, root := range []string{"include", "src", "src/include"} {
		guesses = append(guesses, root+"/"+inc)
	}
	if repoTree == nil {
		return guesses
	}
	seen := make(map[string]struct{})
	var kept []string
	for _, g := range guesses {
		if _, ok := repoTree[g]; !ok {
			continue
		}
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		kept = append(kept, g)
	}
	return kept
}

// normalizeRelative resolves a relative path against srcDir. `..` walks up;
// `.` is dropped; leading `/` is stripped. Kept in sync with Mira's
// _normalize_relative.
func normalizeRelative(srcDir, rel string) string {
	if strings.HasPrefix(rel, "/") {
		return strings.TrimLeft(rel, "/")
	}
	var parts []string
	for _, p := range strings.Split(srcDir, "/") {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}
