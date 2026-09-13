// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package extract

import (
	"strings"
	"testing"
	"time"
)

func TestPythonSymbols(t *testing.T) {
	src := `
import os
import sys as system
from typing import List, Optional
from .relative import thing

def top_level():
    pass

async def async_fn(x, y):
    return x

class Widget:
    def method(self, x):
        return x

    async def amethod(self):
        pass
`
	res := PythonExtractor(src, "x.py")

	wantSyms := map[string]string{
		"top_level": "function",
		"async_fn":  "function",
		"Widget":    "class",
		"method":    "method",
		"amethod":   "method",
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
			t.Errorf("missing symbol %s (%s); got %+v", name, kind, res.Symbols)
		}
	}
}

func TestPythonImports(t *testing.T) {
	src := `
import os
import sys, json
from typing import List, Optional
from .relative import thing
from pkg import (a, b as bee, c)
from wild import *
`
	res := PythonExtractor(src, "x.py")

	wants := []string{"os", "sys", "json", "typing", "typing.List", "typing.Optional", ".relative.thing", "pkg.a", "pkg.b", "pkg.c"}
	for _, w := range wants {
		if !hasImport(res, w) {
			t.Errorf("missing import %q; got %v", w, res.Imports)
		}
	}
	// Wildcard should not appear as an import name.
	for _, imp := range res.Imports {
		if strings.HasSuffix(imp, "*") {
			t.Errorf("wildcard leaked into imports: %q", imp)
		}
	}
}

func TestPythonComments(t *testing.T) {
	src := `# def not_a_def():
x = "def looks_like_def(): pass"
def real_fn():
    pass
`
	res := PythonExtractor(src, "x.py")
	for _, s := range res.Symbols {
		if s.Name == "not_a_def" || s.Name == "looks_like_def" {
			t.Errorf("comment/string leaked as symbol: %+v", s)
		}
	}
	if !hasSymbol(res, "real_fn") {
		t.Error("real function not extracted")
	}
}

func TestPythonMalformed(t *testing.T) {
	// Doesn't panic on garbage.
	_ = PythonExtractor("def (\n  (:!@#$%^\n\n\n", "x.py")
	_ = PythonExtractor("", "x.py")
	_ = PythonExtractor("\x00\x00\x00", "x.py")
}

func TestPythonEmpty(t *testing.T) {
	res := PythonExtractor("", "x.py")
	if len(res.Symbols) != 0 || len(res.Imports) != 0 {
		t.Fatalf("empty file should return empty; got %+v", res)
	}
}

func TestPythonLargeFile(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("def fn_")
		b.WriteString(itoa(i))
		b.WriteString("():\n    pass\n")
	}
	start := time.Now()
	res := PythonExtractor(b.String(), "big.py")
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("500-line python file took %v (>500ms)", d)
	}
	if len(res.Symbols) < 500 {
		t.Errorf("expected 500 symbols, got %d", len(res.Symbols))
	}
}

// small stdlib-free itoa avoids depending on strconv in a test helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
