// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package filetype

import "testing"

func TestLanguageFromPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want Language
	}{
		{"python", "src/foo.py", LangPython},
		{"js", "app.js", LangJavaScript},
		{"jsx", "app.jsx", LangJavaScript},
		{"ts", "app.ts", LangTypeScript},
		{"tsx uppercase", "App.TSX", LangTypeScript},
		{"go", "main.go", LangGo},
		{"cpp .cc", "foo.cc", LangCPP},
		{"c .h", "foo.h", LangC},
		{"cpp .hpp", "foo.hpp", LangCPP},
		{"kotlin script", "build.kts", LangKotlin},
		{"markdown", "README.md", LangMarkdown},
		{"terraform", "main.tf", LangTerraform},
		{"protobuf", "api.proto", LangProtobuf},
		{"nested path", "a/b/c/d.rs", LangRust},
		{"no extension", "Makefile", LangUnknown},
		{"unknown extension", "foo.xyz", LangUnknown},
		{"empty", "", LangUnknown},
		{"dotfile only", ".gitignore", LangUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LanguageFromPath(tc.path); got != tc.want {
				t.Fatalf("LanguageFromPath(%q) = %q; want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestIsIndexablePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"go", "main.go", true},
		{"python", "a/b.py", true},
		{"ts", "x.ts", true},
		{"yaml", "cfg.yaml", true},
		{"yml", "cfg.yml", true},
		{"proto", "svc.proto", true},
		{"graphql", "schema.graphql", true},
		{"terraform", "main.tf", true},
		{"markdown not indexable", "README.md", false},
		{"html not indexable", "index.html", false},
		{"css not indexable", "app.css", false},
		{"xml not indexable", "pom.xml", false},
		{"r not indexable", "script.r", false},
		{"dart not indexable", "main.dart", false},
		{"elixir not indexable", "app.ex", false},
		{"elixir script not indexable", "app.exs", false},
		{"erlang not indexable", "app.erl", false},
		{"haskell not indexable", "app.hs", false},
		{"unknown ext", "foo.bin", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsIndexablePath(tc.path); got != tc.want {
				t.Fatalf("IsIndexablePath(%q) = %v; want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestNormalizeExtension(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"  ", ""},
		{"go", ".go"},
		{".go", ".go"},
		{".GO", ".go"},
		{" .Py ", ".py"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeExtension(tc.in); got != tc.want {
				t.Fatalf("normalizeExtension(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}
