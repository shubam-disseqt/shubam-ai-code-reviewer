// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package extract

import "testing"

func TestRust(t *testing.T) {
	src := `
use std::collections::HashMap;
use crate::a::b;
use crate::x::{y, z::{p, q}};
pub use foo::bar as baz;

pub struct Widget { name: String }
pub enum Color { Red, Green }
pub trait Handler { fn handle(&self); }
impl Widget { pub fn new() -> Self { Widget { name: String::new() } } }
impl Handler for Widget { fn handle(&self) {} }

pub async fn fetch(url: &str) -> Result<String, Error> { Ok(String::new()) }
fn helper() {}
pub(crate) fn scoped() {}
unsafe fn danger() {}
`
	res := RustExtractor(src, "x.rs")

	wantSyms := map[string]string{
		"Widget":  "struct",
		"Color":   "enum",
		"Handler": "trait",
		"fetch":   "function",
		"helper":  "function",
		"scoped":  "function",
		"danger":  "function",
	}
	// Note: `impl Widget { pub fn new() ... }` on one line is intentionally
	// missed — Mira's regex is line-anchored and misses inline-body fns too.
	// Per PORTING.md §7, being more precise here would regress JIT context.
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

	for _, w := range []string{"std::collections::HashMap", "crate::a::b", "crate::x::y", "crate::x::z::p", "crate::x::z::q", "foo::bar"} {
		if !hasImport(res, w) {
			t.Errorf("missing import %q; got %v", w, res.Imports)
		}
	}
}

func TestRustExpandUse(t *testing.T) {
	got := expandRustUse("a::{b, c::{d, e}}")
	want := map[string]bool{"a::b": true, "a::c::d": true, "a::c::e": true}
	if len(got) != len(want) {
		t.Errorf("want %d got %v", len(want), got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected %q", g)
		}
	}

	// self expansion.
	got = expandRustUse("a::b::{self, c}")
	seenSelf, seenC := false, false
	for _, g := range got {
		if g == "a::b" {
			seenSelf = true
		}
		if g == "a::b::c" {
			seenC = true
		}
	}
	if !seenSelf || !seenC {
		t.Errorf("self/child missing: %v", got)
	}

	// Unmatched brace fallback.
	got = expandRustUse("a::{unclosed")
	if len(got) == 0 {
		t.Error("unmatched brace should still return prefix")
	}
}

func TestRustMalformed(t *testing.T) {
	_ = RustExtractor("fn (\nstruct\ntrait\n", "x.rs")
	_ = RustExtractor("", "x.rs")
}
