// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package extract

import (
	"strings"
	"testing"
	"time"
)

func TestGoSymbols(t *testing.T) {
	src := "package p\n" +
		"\n" +
		"import \"fmt\"\n" +
		"\n" +
		"type Widget struct {\n" +
		"    Name string\n" +
		"}\n" +
		"\n" +
		"type Handler interface {\n" +
		"    Handle() error\n" +
		"}\n" +
		"\n" +
		"type Alias = string\n" +
		"type Newtype int\n" +
		"\n" +
		"const K = 1\n" +
		"var V = 2\n" +
		"\n" +
		"const (\n" +
		"    A = 1\n" +
		"    B = 2\n" +
		")\n" +
		"\n" +
		"var (\n" +
		"    x = 1\n" +
		"    y = 2\n" +
		")\n" +
		"\n" +
		"func TopLevel() error { return nil }\n" +
		"\n" +
		"func (w *Widget) Method(x int) string {\n" +
		"    return fmt.Sprintf(\"%d\", x)\n" +
		"}\n"

	res := GoExtractor(src, "x.go")

	wantSyms := map[string]string{
		"Widget":   "struct",
		"Handler":  "interface",
		"Alias":    "type",
		"Newtype":  "type",
		"K":        "constant",
		"V":        "variable",
		"A":        "constant",
		"B":        "constant",
		"x":        "variable",
		"y":        "variable",
		"TopLevel": "function",
		"Method":   "method",
	}
	for name, kind := range wantSyms {
		found := false
		for _, s := range res.Symbols {
			if s.Name == name && s.Kind == kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s (%s); got %+v", name, kind, res.Symbols)
		}
	}

	if !hasImport(res, "fmt") {
		t.Errorf("fmt import missing; got %v", res.Imports)
	}
}

func TestGoImportShapes(t *testing.T) {
	src := "package p\n\n" +
		"import \"single/path\"\n" +
		"import alias \"aliased/path\"\n" +
		"import _ \"blank/path\"\n" +
		"import . \"dot/path\"\n" +
		"\n" +
		"import (\n" +
		"    \"block/one\"\n" +
		"    alias2 \"block/two\"\n" +
		"    _ \"block/blank\"\n" +
		"    . \"block/dot\"\n" +
		"    // comment inside block\n" +
		"    \"block/three\"\n" +
		")\n"

	res := GoExtractor(src, "x.go")
	wants := []string{
		"single/path", "aliased/path", "blank/path", "dot/path",
		"block/one", "block/two", "block/blank", "block/dot", "block/three",
	}
	for _, w := range wants {
		if !hasImport(res, w) {
			t.Errorf("missing import %q; got %v", w, res.Imports)
		}
	}
}

func TestGoStringsDontContaminate(t *testing.T) {
	// Regex-over-whole-file would blow up here — the strings look like Go
	// imports and Go symbol declarations.
	src := "package p\n\n" +
		"import \"real\"\n" +
		"\n" +
		"func F() {\n" +
		"    s := \"import \\\"fake\\\"\"\n" +
		"    t := `import \"another_fake\"\n" +
		"          func FakeInBacktick() {}\n" +
		"          type FakeType struct{}`\n" +
		"    _, _ = s, t\n" +
		"}\n" +
		"\n" +
		"// import \"comment_fake\"\n" +
		"/* import \"block_comment_fake\" */\n" +
		"/*\n" +
		"import \"multiline_block_fake\"\n" +
		"func FakeInBlockComment() {}\n" +
		"*/\n" +
		"func Real() {}\n"

	res := GoExtractor(src, "x.go")

	// Real ones present.
	if !hasImport(res, "real") {
		t.Errorf("real import missing; got %v", res.Imports)
	}
	if !hasSymbol(res, "F") || !hasSymbol(res, "Real") {
		t.Errorf("real symbols missing; got %+v", res.Symbols)
	}
	// Fakes ABSENT.
	for _, bad := range []string{"fake", "another_fake", "comment_fake", "block_comment_fake", "multiline_block_fake"} {
		if hasImport(res, bad) {
			t.Errorf("string/comment leaked as import: %q; got %v", bad, res.Imports)
		}
	}
	for _, bad := range []string{"FakeInBacktick", "FakeType", "FakeInBlockComment"} {
		if hasSymbol(res, bad) {
			t.Errorf("string/comment leaked as symbol: %q; got %+v", bad, res.Symbols)
		}
	}
}

func TestGoReceiverForms(t *testing.T) {
	src := "package p\n" +
		"func (r Receiver) A() {}\n" +
		"func (r *Receiver) B() {}\n" +
		"func (Receiver) C() {}\n" +
		"func D() {}\n"
	res := GoExtractor(src, "x.go")

	for _, name := range []string{"A", "B", "C", "D"} {
		if !hasSymbol(res, name) {
			t.Errorf("missing %s", name)
		}
	}
	// A, B, C are methods; D is function.
	for _, s := range res.Symbols {
		switch s.Name {
		case "A", "B", "C":
			if s.Kind != "method" {
				t.Errorf("%s should be method, got %s", s.Name, s.Kind)
			}
			if !strings.HasPrefix(s.Signature, "(") {
				t.Errorf("%s signature missing receiver: %q", s.Name, s.Signature)
			}
		case "D":
			if s.Kind != "function" {
				t.Errorf("D should be function, got %s", s.Kind)
			}
		}
	}
}

func TestGoOneLineImportBlock(t *testing.T) {
	src := "package p\nimport ( \"a\" \"b\" )\n"
	res := GoExtractor(src, "x.go")
	if !hasImport(res, "a") || !hasImport(res, "b") {
		t.Errorf("one-line block missed; got %v", res.Imports)
	}
}

func TestGoMalformed(t *testing.T) {
	// Truncated imports, unbalanced braces, garbage.
	_ = GoExtractor("package p\nimport (\n  \"unclosed\n", "x.go")
	_ = GoExtractor("package p\nfunc {{{", "x.go")
	_ = GoExtractor("", "x.go")
	_ = GoExtractor("\x00binary\x01garbage", "x.go")
}

func TestGoEmpty(t *testing.T) {
	res := GoExtractor("", "x.go")
	if len(res.Symbols) != 0 || len(res.Imports) != 0 {
		t.Fatalf("empty got %+v", res)
	}
}

func TestGoLargeFile(t *testing.T) {
	var b strings.Builder
	b.WriteString("package p\n\nimport \"fmt\"\n\n")
	for i := 0; i < 500; i++ {
		b.WriteString("func Fn")
		b.WriteString(itoa(i))
		b.WriteString("(x int) int { return x }\n")
	}
	start := time.Now()
	res := GoExtractor(b.String(), "big.go")
	if d := time.Since(start); d > time.Second {
		t.Errorf("500-line go took %v", d)
	}
	if len(res.Symbols) < 500 {
		t.Errorf("expected 500 symbols, got %d", len(res.Symbols))
	}
}

func TestGoStripGoNoise(t *testing.T) {
	// Direct probes for the state helper.
	tests := []struct {
		in                string
		inBlock, inRaw    bool
		wantOut           string
		wantBlock, wantRw bool
	}{
		{`x := "hello" // comment`, false, false, `x := "hello"           `, false, false},
		{`/* start`, false, false, `        `, true, false},
		{`middle */ real`, true, false, `          real`, false, false},
		{"back`raw`", false, false, "back     ", false, false},
	}
	for _, tc := range tests {
		out, bo, ro := stripGoNoise(tc.in, tc.inBlock, tc.inRaw)
		if out != tc.wantOut {
			t.Errorf("stripGoNoise(%q) out=%q want=%q", tc.in, out, tc.wantOut)
		}
		if bo != tc.wantBlock || ro != tc.wantRw {
			t.Errorf("stripGoNoise(%q) blk=%v raw=%v want blk=%v raw=%v", tc.in, bo, ro, tc.wantBlock, tc.wantRw)
		}
	}
}
