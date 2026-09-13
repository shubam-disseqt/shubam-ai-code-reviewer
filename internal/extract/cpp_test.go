// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package extract

import "testing"

func TestCPP(t *testing.T) {
	src := `
#include "widget.h"
#include "util/helpers.h"
#include <vector>
#include <string>

template<typename T>
class Widget {
public:
    Widget();
    void reset();
};

struct Point { int x; int y; };

int compute(int a, int b) {
    return a + b;
}

static inline void helper() {
}
`
	res := CPPExtractor(src, "x.cpp")

	// Local includes present, system NOT.
	if !hasImport(res, "widget.h") {
		t.Errorf("widget.h missing; got %v", res.Imports)
	}
	if !hasImport(res, "util/helpers.h") {
		t.Errorf("util/helpers.h missing; got %v", res.Imports)
	}
	for _, sys := range []string{"vector", "string"} {
		if hasImport(res, sys) {
			t.Errorf("system include %q leaked into imports", sys)
		}
	}

	wantSyms := map[string]string{
		"Widget":  "class",
		"Point":   "struct",
		"compute": "function",
		"helper":  "function",
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
}

func TestCPPMalformed(t *testing.T) {
	_ = CPPExtractor("class {\nstruct\n#include", "x.cpp")
	_ = CPPExtractor("", "x.cpp")
}
