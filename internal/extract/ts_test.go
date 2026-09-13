// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package extract

import "testing"

func TestTypeScriptExtras(t *testing.T) {
	src := `
import { X } from './x';
export interface Widget<T> { name: T; }
type Handler = (x: number) => string;
export const enum Color { Red, Green, Blue }
enum Priority { Low, High }
export function f<T>(x: T): T { return x; }
`
	res := TypeScriptExtractor(src, "x.ts")

	want := map[string]string{
		"Widget":   "interface",
		"Handler":  "type",
		"Color":    "enum",
		"Priority": "enum",
		"f":        "function",
	}
	for name, kind := range want {
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
	if !hasImport(res, "./x") {
		t.Error("missing ./x import")
	}
}

func TestTypeScriptMalformed(t *testing.T) {
	_ = TypeScriptExtractor("interface { }\ntype = ;\n", "x.ts")
	_ = TypeScriptExtractor("", "x.ts")
}
