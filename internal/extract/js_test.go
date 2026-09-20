// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package extract

import (
	"strings"
	"testing"
	"time"
)

func TestJSSymbols(t *testing.T) {
	src := `
import { thing } from './mod';
import def from 'pkg';
const cjs = require('./x');
const { y } = require("y");

export function f(a, b) { return a + b; }
export default class C {
  method() {}
}
const arrow = () => 1;
export const arrow2 = async (x) => x;
let v = 5;
var w = 'hi';
`
	res := JavaScriptExtractor(src, "x.js")
	wantSyms := map[string]string{
		"f":      "function",
		"C":      "class",
		"arrow":  "function",
		"arrow2": "function",
		"cjs":    "constant",
		"v":      "variable",
		"w":      "variable",
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
	// Imports.
	for _, want := range []string{"./mod", "pkg", "./x", "y"} {
		if !hasImport(res, want) {
			t.Errorf("missing import %q; got %v", want, res.Imports)
		}
	}
}

func TestJSDynamicImport(t *testing.T) {
	src := `const m = await import('lazy');`
	res := JavaScriptExtractor(src, "x.js")
	if !hasImport(res, "lazy") {
		t.Errorf("missing dynamic import; got %v", res.Imports)
	}
}

func TestJSCommentsAndStrings(t *testing.T) {
	src := `
// import 'not-a-real-import';
const s = "require('fake')";
const t = 'import "also fake"';
import real from 'real';
function realFn() {}
`
	res := JavaScriptExtractor(src, "x.js")
	if !hasImport(res, "real") {
		t.Error("real import missed")
	}
	for _, imp := range res.Imports {
		if imp == "not-a-real-import" || imp == "fake" || imp == "also fake" {
			t.Errorf("comment/string leaked into imports: %q", imp)
		}
	}
	if !hasSymbol(res, "realFn") {
		t.Error("realFn missing")
	}
}

func TestJSMalformed(t *testing.T) {
	_ = JavaScriptExtractor("function (((\nconst = ;", "x.js")
	_ = JavaScriptExtractor("", "x.js")
}

func TestBlankStringsAndStripComment(t *testing.T) {
	// Cover the escape / unterminated / tick branches of blankStrings and
	// the string-context branches of stripLineComment.
	inputs := []string{
		`'a' + "b" + ` + "`c`",
		`x = 'a\'b'`,
		`s = "hello \"world\""`,
		"back`tick escape \\` inside`",
		`"unterm`,
		`no strings here`,
	}
	for _, in := range inputs {
		out := blankStrings(in)
		if len(out) != len(in) {
			t.Errorf("blankStrings should preserve length: %q -> %q", in, out)
		}
	}
	// stripLineComment: strings/backticks/single-quotes protecting //.
	if got := stripLineComment(`x = "y" // trailing`); got != `x = "y" ` {
		t.Errorf("stripLineComment trailing wrong: %q", got)
	}
	if got := stripLineComment("`back // tick`"); got != "`back // tick`" {
		t.Errorf("stripLineComment backtick wrong: %q", got)
	}
	if got := stripLineComment(`'x' // z`); got != `'x' ` {
		t.Errorf("stripLineComment single-quote wrong: %q", got)
	}
	if got := stripLineComment(`no comment here`); got != `no comment here` {
		t.Errorf("stripLineComment plain wrong: %q", got)
	}
}

func TestJSLargeFile(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("function fn")
		b.WriteString(itoa(i))
		b.WriteString("() {}\n")
	}
	start := time.Now()
	res := JavaScriptExtractor(b.String(), "big.js")
	// Perf guard; shared GH runners (esp. Windows) can be 2-3x slower
	// than a local dev laptop. 2s is the "clearly regressed" boundary.
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("500-line js took %v (>2s)", d)
	}
	if len(res.Symbols) < 500 {
		t.Errorf("expected 500 symbols, got %d", len(res.Symbols))
	}
}
