// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/core/file_types.py under Apache License 2.0.

// Package filetype maps file paths to their source-code language and reports
// whether a path is one of the languages the indexer supports.
package filetype

import (
	"path/filepath"
	"strings"
)

// Language identifies a source language or config format.
type Language string

// Known languages. The zero value ("") means "unknown".
const (
	LangUnknown    Language = ""
	LangPython     Language = "python"
	LangJavaScript Language = "javascript"
	LangTypeScript Language = "typescript"
	LangRuby       Language = "ruby"
	LangGo         Language = "go"
	LangRust       Language = "rust"
	LangJava       Language = "java"
	LangKotlin     Language = "kotlin"
	LangCSharp     Language = "csharp"
	LangCPP        Language = "cpp"
	LangC          Language = "c"
	LangSwift      Language = "swift"
	LangPHP        Language = "php"
	LangScala      Language = "scala"
	LangBash       Language = "bash"
	LangZsh        Language = "zsh"
	LangYAML       Language = "yaml"
	LangJSON       Language = "json"
	LangTOML       Language = "toml"
	LangXML        Language = "xml"
	LangHTML       Language = "html"
	LangCSS        Language = "css"
	LangSCSS       Language = "scss"
	LangSQL        Language = "sql"
	LangMarkdown   Language = "markdown"
	LangR          Language = "r"
	LangDart       Language = "dart"
	LangLua        Language = "lua"
	LangElixir     Language = "elixir"
	LangErlang     Language = "erlang"
	LangHaskell    Language = "haskell"
	LangOCaml      Language = "ocaml"
	LangClojure    Language = "clojure"
	LangVim        Language = "vim"
	LangTerraform  Language = "terraform"
	LangGraphQL    Language = "graphql"
	LangProtobuf   Language = "protobuf"
)

// extensionLanguages maps normalized (leading-dot, lowercase) extensions to
// their canonical Language.
var extensionLanguages = map[string]Language{
	".py":      LangPython,
	".js":      LangJavaScript,
	".jsx":     LangJavaScript,
	".ts":      LangTypeScript,
	".tsx":     LangTypeScript,
	".rb":      LangRuby,
	".go":      LangGo,
	".rs":      LangRust,
	".java":    LangJava,
	".kt":      LangKotlin,
	".kts":     LangKotlin,
	".cs":      LangCSharp,
	".cpp":     LangCPP,
	".cc":      LangCPP,
	".c":       LangC,
	".h":       LangC,
	".hpp":     LangCPP,
	".swift":   LangSwift,
	".php":     LangPHP,
	".scala":   LangScala,
	".sh":      LangBash,
	".bash":    LangBash,
	".zsh":     LangZsh,
	".yml":     LangYAML,
	".yaml":    LangYAML,
	".json":    LangJSON,
	".toml":    LangTOML,
	".xml":     LangXML,
	".html":    LangHTML,
	".css":     LangCSS,
	".scss":    LangSCSS,
	".sql":     LangSQL,
	".md":      LangMarkdown,
	".r":       LangR,
	".dart":    LangDart,
	".lua":     LangLua,
	".ex":      LangElixir,
	".exs":     LangElixir,
	".erl":     LangErlang,
	".hs":      LangHaskell,
	".ml":      LangOCaml,
	".clj":     LangClojure,
	".vim":     LangVim,
	".tf":      LangTerraform,
	".graphql": LangGraphQL,
	".proto":   LangProtobuf,
}

// indexableLanguages is the set of languages the indexer understands.
var indexableLanguages = map[Language]struct{}{
	LangPython:     {},
	LangJavaScript: {},
	LangTypeScript: {},
	LangRuby:       {},
	LangGo:         {},
	LangRust:       {},
	LangJava:       {},
	LangKotlin:     {},
	LangCSharp:     {},
	LangCPP:        {},
	LangC:          {},
	LangSwift:      {},
	LangPHP:        {},
	LangScala:      {},
	LangBash:       {},
	LangZsh:        {},
	LangYAML:       {},
	LangJSON:       {},
	LangTOML:       {},
	LangSQL:        {},
	LangLua:        {},
	LangTerraform:  {},
	LangGraphQL:    {},
	LangProtobuf:   {},
}

// normalizeExtension lowercases and prepends a dot if missing. Empty input
// yields an empty result.
func normalizeExtension(ext string) string {
	v := strings.ToLower(strings.TrimSpace(ext))
	if v == "" {
		return ""
	}
	if strings.HasPrefix(v, ".") {
		return v
	}
	return "." + v
}

// extensionFromPath returns the normalized final suffix of path.
func extensionFromPath(path string) string {
	return normalizeExtension(filepath.Ext(path))
}

// LanguageFromPath returns the canonical Language for path's final suffix,
// or LangUnknown if the extension is not recognized.
func LanguageFromPath(path string) Language {
	return extensionLanguages[extensionFromPath(path)]
}

// IsIndexablePath reports whether path has an extension the indexer supports.
func IsIndexablePath(path string) bool {
	lang, ok := extensionLanguages[extensionFromPath(path)]
	if !ok {
		return false
	}
	_, ok = indexableLanguages[lang]
	return ok
}
