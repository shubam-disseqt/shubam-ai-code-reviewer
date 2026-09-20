// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package extract

import (
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/filetype"
)

func TestDispatch(t *testing.T) {
	// Dispatch happy paths per language: each language's extractor gets a
	// tiny fixture and we assert at least one symbol / import comes back.
	cases := []struct {
		lang    filetype.Language
		content string
		wantSym string
		wantImp string
	}{
		{filetype.LangPython, "import os\ndef greet():\n    pass\n", "greet", "os"},
		{filetype.LangJavaScript, "import x from 'y'\nfunction f(){}\n", "f", "y"},
		{filetype.LangTypeScript, "import x from 'y'\ninterface I {}\n", "I", "y"},
		{filetype.LangRuby, "require 'x'\ndef g; end\n", "g", "x"},
		{filetype.LangGo, "package p\nimport \"fmt\"\nfunc F(){}\n", "F", "fmt"},
		{filetype.LangRust, "use a::b;\nfn f(){}\n", "f", "a::b"},
		{filetype.LangJava, "import a.b.C;\npublic class Foo {}\n", "Foo", "a.b.C"},
		{filetype.LangCPP, `#include "x.h"` + "\nclass C {};\n", "C", "x.h"},
	}
	for _, tc := range cases {
		t.Run(string(tc.lang), func(t *testing.T) {
			res := Extract(tc.lang, tc.content, "test")
			if !hasSymbol(res, tc.wantSym) {
				t.Errorf("missing symbol %q; got %+v", tc.wantSym, res.Symbols)
			}
			if !hasImport(res, tc.wantImp) {
				t.Errorf("missing import %q; got %+v", tc.wantImp, res.Imports)
			}
		})
	}
}

func TestDispatchUnknown(t *testing.T) {
	res := Extract(filetype.LangYAML, "key: value", "x.yaml")
	if len(res.Symbols) != 0 || len(res.Imports) != 0 {
		t.Fatalf("unknown lang should return empty; got %+v", res)
	}
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r[filetype.LangGo] == nil {
		t.Fatal("Go extractor missing from registry")
	}
	if r[filetype.LangPython] == nil {
		t.Fatal("Python extractor missing from registry")
	}
	// Registries are independent copies (map value semantics).
	if len(r) < 8 {
		t.Fatalf("expected at least 8 extractors, got %d", len(r))
	}
}

func TestDedupe(t *testing.T) {
	got := dedupe([]string{"a", "b", "a", "c", "b"})
	want := []string{"a", "b", "c"}
	if !equal(got, want) {
		t.Fatalf("dedupe: got %v want %v", got, want)
	}
	if dedupe(nil) != nil {
		t.Fatal("dedupe(nil) should stay nil")
	}
}

func hasSymbol(r Result, name string) bool {
	for _, s := range r.Symbols {
		if s.Name == name {
			return true
		}
	}
	return false
}

func hasImport(r Result, name string) bool {
	for _, i := range r.Imports {
		if i == name {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
