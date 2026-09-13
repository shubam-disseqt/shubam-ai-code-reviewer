// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package extract

import "testing"

func TestRuby(t *testing.T) {
	src := `
require 'json'
require_relative './helper'

module App
  class Widget
    def initialize(name)
      @name = name
    end

    def self.build
      new('default')
    end
  end
end
`
	res := RubyExtractor(src, "x.rb")

	wantSyms := map[string]string{
		"App":        "module",
		"Widget":     "class",
		"initialize": "function",
		"build":      "function",
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
	for _, w := range []string{"json", "./helper"} {
		if !hasImport(res, w) {
			t.Errorf("missing import %q; got %v", w, res.Imports)
		}
	}
}

func TestRubyCommentsAndStrings(t *testing.T) {
	src := `
# def not_a_def; end
x = "def string_looks_like_def"
def real; end
`
	res := RubyExtractor(src, "x.rb")
	for _, s := range res.Symbols {
		if s.Name == "not_a_def" || s.Name == "string_looks_like_def" {
			t.Errorf("comment/string leaked: %+v", s)
		}
	}
	if !hasSymbol(res, "real") {
		t.Error("real def missing")
	}
}

func TestRubyMalformed(t *testing.T) {
	_ = RubyExtractor("def\nclass\nmodule\n", "x.rb")
	_ = RubyExtractor("", "x.rb")
}
