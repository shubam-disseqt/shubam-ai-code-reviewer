// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package index

import (
	"strings"
	"testing"
)

func TestParseSummarizeResponseHappy(t *testing.T) {
	t.Parallel()
	raw := `{"files":[{"path":"a.go","language":"go","summary":"tiny","symbols":[{"name":"F","kind":"function"}]}]}`
	files := parseSummarizeResponse(raw)
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Path != "a.go" || files[0].Language != "go" || files[0].Summary != "tiny" {
		t.Errorf("unexpected parse: %+v", files[0])
	}
	if len(files[0].Symbols) != 1 || files[0].Symbols[0].Name != "F" {
		t.Errorf("symbols wrong: %+v", files[0].Symbols)
	}
}

func TestParseSummarizeResponseCodeFences(t *testing.T) {
	t.Parallel()
	raw := "```json\n" + `{"files":[{"path":"a.go","summary":"x"}]}` + "\n```"
	files := parseSummarizeResponse(raw)
	if len(files) != 1 || files[0].Path != "a.go" {
		t.Errorf("code fences not stripped: %+v", files)
	}
}

func TestParseSummarizeResponseThinkBlock(t *testing.T) {
	t.Parallel()
	raw := "<think>reasoning here</think>" + `{"files":[{"path":"a.go"}]}`
	files := parseSummarizeResponse(raw)
	if len(files) != 1 {
		t.Errorf("think block not stripped: %+v", files)
	}
}

func TestParseSummarizeResponseLoneBackslashRepair(t *testing.T) {
	t.Parallel()
	// DeepSeek-style unescaped PHP namespace inside a string literal.
	raw := `{"files":[{"path":"a.php","summary":"uses \App\Models\User"}]}`
	files := parseSummarizeResponse(raw)
	if len(files) != 1 {
		t.Fatalf("repair pass failed: %+v", files)
	}
	if !strings.Contains(files[0].Summary, "App") {
		t.Errorf("summary corrupted after repair: %q", files[0].Summary)
	}
}

func TestParseSummarizeResponseMalformedTolerated(t *testing.T) {
	t.Parallel()
	// Utter garbage → empty result, no panic.
	if got := parseSummarizeResponse("not json at all"); got != nil {
		t.Errorf("malformed input should yield nil, got %+v", got)
	}
}

func TestParseSummarizeResponseBareArray(t *testing.T) {
	t.Parallel()
	// Some models skip the envelope and emit a bare array.
	raw := `[{"path":"a.go","summary":"x"}]`
	files := parseSummarizeResponse(raw)
	if len(files) != 1 || files[0].Path != "a.go" {
		t.Errorf("bare array not accepted: %+v", files)
	}
}

func TestEscapeLoneBackslashes(t *testing.T) {
	t.Parallel()
	// Valid escape sequences pass through.
	in := `{"k":"a\nb\tc\"d"}`
	if escapeLoneBackslashes(in) != in {
		t.Errorf("valid escapes were mangled: %q → %q", in, escapeLoneBackslashes(in))
	}
	// Lone backslash → doubled. Confirm by re-parsing.
	broken := `{"k":"C:\Users\me"}`
	fixed := escapeLoneBackslashes(broken)
	if !strings.Contains(fixed, `\\U`) {
		t.Errorf("expected doubled backslash, got %q", fixed)
	}
}

func TestStripThinkBlocks(t *testing.T) {
	t.Parallel()
	in := "before<think>stuff</think>after<think>more</think>end"
	if got := stripThinkBlocks(in); got != "beforeafterend" {
		t.Errorf("stripThinkBlocks = %q", got)
	}
}

func TestStripCodeFences(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"```json\n{}\n```": "{}",
		"```\n[]\n```":     "[]",
		"no fences":        "no fences",
		"   plain   ":      "plain",
	}
	for in, want := range cases {
		if got := stripCodeFences(in); got != want {
			t.Errorf("stripCodeFences(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildFileSummary(t *testing.T) {
	t.Parallel()
	data := summarizeFileJSON{
		Language: "go",
		Summary:  "does stuff",
		Symbols: []summarizeSymbol{
			{Name: "F", Kind: "", Signature: "func F()", Description: "d"},
			{Name: "", Kind: "function"}, // dropped: no name
		},
		Imports: []string{"other.go"},
		SymbolReferences: []symbolRefBlock{
			{Source: "F", Calls: []refCallBody{{Path: "b.go", Symbol: "G"}, {Path: "", Symbol: ""}}},
		},
		ExternalRefs: []externalRefBlock{
			{Kind: "npm_package", Target: "react"},
			{Kind: "", Target: "dropped"},
		},
	}
	fs := buildFileSummary("a.go", "package a\n", data)
	if fs.Path != "a.go" || fs.Summary != "does stuff" {
		t.Errorf("basic fields wrong: %+v", fs)
	}
	if len(fs.Symbols) != 1 || fs.Symbols[0].Kind != "function" {
		t.Errorf("symbols default kind lost: %+v", fs.Symbols)
	}
	if len(fs.SymbolRefs) != 1 || fs.SymbolRefs[0].TargetPath != "b.go" {
		t.Errorf("symbol refs wrong: %+v", fs.SymbolRefs)
	}
	if len(fs.ExternalRefs) != 1 || fs.ExternalRefs[0].Kind != "npm_package" {
		t.Errorf("external refs wrong: %+v", fs.ExternalRefs)
	}
	if fs.ContentHash == "" || fs.LOC == 0 {
		t.Errorf("content hash / LOC not computed: %+v", fs)
	}
}
