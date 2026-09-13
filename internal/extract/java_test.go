// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package extract

import "testing"

func TestJava(t *testing.T) {
	src := `
package com.example;

import java.util.List;
import java.util.Map;
import static com.example.Util.helper;
import com.example.wild.*;

public class Widget {
    public String getName() { return name; }
    private void reset() {}
}

interface Handler {
    void handle(Event e);
}

enum Color { RED, GREEN }
`
	res := JavaExtractor(src, "X.java")
	wantSyms := map[string]string{
		"Widget":  "class",
		"Handler": "interface",
		"Color":   "enum",
		"getName": "method",
		"reset":   "method",
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
	for _, w := range []string{"java.util.List", "java.util.Map", "com.example.Util.helper"} {
		if !hasImport(res, w) {
			t.Errorf("missing import %q; got %v", w, res.Imports)
		}
	}
	// Wildcard imports MUST be rejected.
	for _, imp := range res.Imports {
		if imp == "com.example.wild.*" || imp == "com.example.wild" {
			// The regex allows the dotted FQN but rejects `.*` suffix;
			// unqualified reject of the whole line is more conservative.
			// Either way, `.*` must never appear in the list.
			if imp == "com.example.wild.*" {
				t.Errorf("wildcard import leaked: %q", imp)
			}
		}
	}
}

func TestJavaMalformed(t *testing.T) {
	_ = JavaExtractor("public class {\n  public foo(", "X.java")
	_ = JavaExtractor("", "X.java")
}

func TestJavaReservedNamesNotSymbols(t *testing.T) {
	src := `
public class X {
    void method() {
        if (true) { return; }
        for (int i = 0; i < 10; i++) {}
        while (true) {}
        switch (x) { case 1: break; }
    }
}
`
	res := JavaExtractor(src, "X.java")
	for _, b := range []string{"if", "for", "while", "switch"} {
		if hasSymbol(res, b) {
			t.Errorf("reserved word %q leaked as symbol", b)
		}
	}
	if !hasSymbol(res, "method") {
		t.Error("real method missing")
	}
}

func TestJavaRecord(t *testing.T) {
	res := JavaExtractor(`public record Point(int x, int y) {}`, "P.java")
	found := false
	for _, s := range res.Symbols {
		if s.Name == "Point" && s.Kind == "class" {
			found = true
		}
	}
	if !found {
		t.Errorf("record didn't map to class: %+v", res.Symbols)
	}
}

func TestJavaWildcardRejected(t *testing.T) {
	res := JavaExtractor(`import a.b.*;`, "X.java")
	for _, imp := range res.Imports {
		if imp == "a.b.*" {
			t.Errorf("wildcard leaked: %q", imp)
		}
	}
}
