// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package reviewctx

import (
	"reflect"
	"sort"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/filetype"
)

func TestExtractImportCandidates_Python(t *testing.T) {
	src := `
from pkg.mod import thing
import top_mod
from . import sibling
`
	got := ExtractImportCandidates(src, filetype.LangPython, "app/main.py", nil, true, "")
	want := []string{
		// from pkg.mod import
		"pkg/mod.py", "pkg/mod/__init__.py",
		"app/pkg/mod.py", "app/pkg/mod/__init__.py",
		"src/pkg/mod.py", "src/pkg/mod/__init__.py",
		"lib/pkg/mod.py", "lib/pkg/mod/__init__.py",
		// import top_mod
		"top_mod.py", "top_mod/__init__.py",
		"app/top_mod.py", "app/top_mod/__init__.py",
		"src/top_mod.py", "src/top_mod/__init__.py",
		"lib/top_mod.py", "lib/top_mod/__init__.py",
	}
	assertStringSliceEqual(t, got, want)
}

func TestExtractImportCandidates_JSRelativeOnly(t *testing.T) {
	src := `
import a from 'react';        // bare -> skip
import b from './helper';
import c from '../lib/util.ts';
require('./legacy');
`
	got := ExtractImportCandidates(src, filetype.LangJavaScript, "src/pkg/x.ts", nil, true, "")
	if len(got) == 0 {
		t.Fatalf("expected relative import candidates, got none")
	}
	// Bare 'react' must not appear.
	for _, c := range got {
		if c == "react" {
			t.Fatalf("bare npm import leaked into candidates: %v", got)
		}
	}
	// './helper' resolves under src/pkg/.
	if !containsPrefix(got, "src/pkg/helper.") {
		t.Errorf("expected src/pkg/helper.* candidate, got %v", got)
	}
	// '../lib/util.ts' resolves under src/ (parent of src/pkg/).
	if !contains(got, "src/lib/util.ts") {
		t.Errorf("expected src/lib/util.ts, got %v", got)
	}
	// './legacy' -> extension guesses.
	if !containsPrefix(got, "src/pkg/legacy.") {
		t.Errorf("expected src/pkg/legacy.* candidate, got %v", got)
	}
}

func TestExtractImportCandidates_Ruby(t *testing.T) {
	src := `
require_relative "./lib/helper"
require "some_gem"
`
	got := ExtractImportCandidates(src, filetype.LangRuby, "app/main.rb", nil, true, "")
	// require_relative resolves against app/.
	if !contains(got, "app/lib/helper.rb") {
		t.Errorf("expected app/lib/helper.rb, got %v", got)
	}
	// Bare gem name is still captured (Mira captures it), normalized under
	// the source dir — Mira's _candidates_ruby always resolves against src_dir.
	if !contains(got, "app/some_gem.rb") {
		t.Errorf("expected app/some_gem.rb, got %v", got)
	}
}

func TestExtractImportCandidates_Java(t *testing.T) {
	src := `
package com.example.app;
import com.example.helper.Widget;
import static com.example.util.Codec.encode;
import com.other.*;
`
	tree := map[string]struct{}{
		"src/main/java/com/example/helper/Widget.java": {},
		"src/main/java/com/example/util/Codec.java":    {},
		"src/main/java/com/other/Whatever.java":        {},
	}
	got := ExtractImportCandidates(src, filetype.LangJava, "src/main/java/com/example/app/App.java", tree, true, "")
	if !contains(got, "src/main/java/com/example/helper/Widget.java") {
		t.Errorf("missing Widget: %v", got)
	}
	// `import static ...Codec.encode` -> Codec class, not encode symbol.
	if !contains(got, "src/main/java/com/example/util/Codec.java") {
		t.Errorf("missing Codec (static import unwrapping): %v", got)
	}
	// Wildcard imports are dropped.
	for _, c := range got {
		if c == "src/main/java/com/other/Whatever.java" {
			t.Fatalf("wildcard import produced a candidate: %v", got)
		}
	}
}

func TestExtractImportCandidates_JavaGoDisabled(t *testing.T) {
	src := `import com.example.Foo;`
	tree := map[string]struct{}{"Foo.java": {}}
	if got := ExtractImportCandidates(src, filetype.LangJava, "src/App.java", tree, false, ""); len(got) != 0 {
		t.Errorf("EnableJavaGo=false must skip Java; got %v", got)
	}

	goSrc := `package main
import "example.com/mymod/pkg/x"
`
	goTree := map[string]struct{}{
		"go.mod":     {},
		"pkg/x/x.go": {},
	}
	if got := ExtractImportCandidates(goSrc, filetype.LangGo, "main.go", goTree, false, "example.com/mymod"); len(got) != 0 {
		t.Errorf("EnableJavaGo=false must skip Go; got %v", got)
	}
}

func TestExtractImportCandidates_GoWithModule(t *testing.T) {
	// Note: no trailing comments on import lines. Mira's regex requires the
	// line to end right after the quoted path; comments after would be dropped.
	src := `package main

import (
	"fmt"
	"net/http"
	"github.com/x/y/pkg/util"
	"example.com/mymod/pkg/x"
	alias "example.com/mymod/pkg/y"
	"unused"
)

func main() { fmt.Println("has \"import\" in a string, must not fool the parser") }
`
	tree := map[string]struct{}{
		"go.mod":          {},
		"pkg/x/x.go":      {},
		"pkg/x/util.go":   {},
		"pkg/y/y.go":      {},
		"pkg/y/y_test.go": {},
	}
	got := ExtractImportCandidates(src, filetype.LangGo, "main.go", tree, true, "example.com/mymod")
	// stdlib and external must not appear
	for _, c := range got {
		if c == "fmt" || c == "net/http" {
			t.Fatalf("stdlib leaked: %v", got)
		}
	}
	if !contains(got, "pkg/x/x.go") {
		t.Errorf("missing pkg/x/x.go: %v", got)
	}
	if !contains(got, "pkg/y/y.go") {
		t.Errorf("missing pkg/y/y.go: %v", got)
	}
	// _test.go must be excluded.
	if contains(got, "pkg/y/y_test.go") {
		t.Errorf("_test.go leaked: %v", got)
	}
}

func TestExtractImportCandidates_GoTailMatch(t *testing.T) {
	src := `package main
import "example.com/foo/pkg/subpkg"
`
	tree := map[string]struct{}{
		"internal/foo/pkg/subpkg/main.go":      {},
		"internal/foo/pkg/subpkg/main_test.go": {},
	}
	got := ExtractImportCandidates(src, filetype.LangGo, "main.go", tree, true, "")
	if !contains(got, "internal/foo/pkg/subpkg/main.go") {
		t.Errorf("tail-match failed: %v", got)
	}
	if contains(got, "internal/foo/pkg/subpkg/main_test.go") {
		t.Errorf("_test.go leaked in tail-match: %v", got)
	}
}

func TestExtractImportCandidates_GoStringsDontFoolResolver(t *testing.T) {
	// The critical PORTING.md gotcha: a whole-file regex would match every
	// quoted string in this file.
	src := `package main

import "fmt"

func main() {
	tag := ` + "`json:\"foo\"`" + `
	fmt.Println("import \"decoy\"", tag)
}
`
	// stdlib "fmt" -> skipped. If our state machine were confused, it would
	// yank strings from the function body.
	got := ExtractImportCandidates(src, filetype.LangGo, "main.go", map[string]struct{}{}, true, "")
	if len(got) != 0 {
		t.Fatalf("state machine confused by strings; got %v", got)
	}
}

func TestExtractImportCandidates_Rust(t *testing.T) {
	src := `
use crate::foo::bar::Baz;
use super::helper;
use std::collections::HashMap;
use serde::Serialize;
`
	tree := map[string]struct{}{
		"src/foo/bar.rs": {},
		"src/foo/mod.rs": {},
		"src/helper.rs":  {},
		"src/lib.rs":     {},
	}
	got := ExtractImportCandidates(src, filetype.LangRust, "src/mymod/main.rs", tree, true, "")
	if !contains(got, "src/foo/bar.rs") {
		t.Errorf("expected src/foo/bar.rs, got %v", got)
	}
	// super:: -> parent of src/mymod/ == src/, helper resolves.
	if !contains(got, "src/helper.rs") {
		t.Errorf("expected src/helper.rs from super::helper, got %v", got)
	}
	// std is filtered out.
	for _, c := range got {
		if c == "std/collections.rs" || c == "collections.rs" {
			t.Fatalf("std leaked: %v", got)
		}
	}
}

func TestExtractImportCandidates_CXX(t *testing.T) {
	src := `
#include "utils/log.hpp"
#include <vector>
`
	tree := map[string]struct{}{
		"utils/log.hpp": {},
	}
	got := ExtractImportCandidates(src, filetype.LangCPP, "src/main.cpp", tree, true, "")
	if !contains(got, "utils/log.hpp") {
		t.Errorf("literal include miss: %v", got)
	}
	// Angle-bracket includes are dropped.
	for _, c := range got {
		if c == "vector" {
			t.Fatalf("system header leaked: %v", got)
		}
	}
}

func TestExtractImportCandidates_UnknownLanguage(t *testing.T) {
	got := ExtractImportCandidates("anything at all", filetype.LangHTML, "foo.html", nil, true, "")
	if len(got) != 0 {
		t.Errorf("unknown language must yield no candidates; got %v", got)
	}
}

func TestParseGoModule(t *testing.T) {
	m := ParseGoModule("module example.com/mymod\n\ngo 1.21\n")
	if m != "example.com/mymod" {
		t.Errorf("got %q", m)
	}
	if got := ParseGoModule("go 1.21\n"); got != "" {
		t.Errorf("no-module input got %q", got)
	}
}

func TestNormalizeRelative(t *testing.T) {
	cases := []struct{ srcDir, rel, want string }{
		{"src/pkg", "./foo", "src/pkg/foo"},
		{"src/pkg", "../lib/x", "src/lib/x"},
		{"src/pkg/deep", "../../top", "src/top"},
		{"src/pkg", "/absolute/path", "absolute/path"},
		{".", "./foo", "foo"},
	}
	for _, c := range cases {
		if got := normalizeRelative(c.srcDir, c.rel); got != c.want {
			t.Errorf("normalizeRelative(%q,%q)=%q want %q", c.srcDir, c.rel, got, c.want)
		}
	}
}

func TestPathOverlap(t *testing.T) {
	if got := pathOverlap("src/main/java/com/example/util/Codec.java", "com.example.util"); got != 3 {
		t.Errorf("overlap=%d", got)
	}
	if got := pathOverlap("anything", ""); got != 0 {
		t.Errorf("empty pkg must overlap 0, got %d", got)
	}
}

// --- helpers ---

func contains(xs []string, needle string) bool {
	for _, x := range xs {
		if x == needle {
			return true
		}
	}
	return false
}

func containsPrefix(xs []string, prefix string) bool {
	for _, x := range xs {
		if len(x) >= len(prefix) && x[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func assertStringSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if !reflect.DeepEqual(g, w) {
		t.Errorf("candidate slice mismatch\n got: %v\nwant: %v", got, want)
	}
}
