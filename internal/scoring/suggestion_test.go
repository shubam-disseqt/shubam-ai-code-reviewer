// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scoring

import (
	"reflect"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
)

func TestIsSuggestion(t *testing.T) {
	base := model.LlmComment{
		Severity:       "low",
		Category:       "style",
		SuggestionCode: "x := 1",
	}
	tests := []struct {
		name string
		in   model.LlmComment
		want bool
	}{
		{"low style with code", base, true},
		{"low maintainability with code", func() model.LlmComment { c := base; c.Category = "maintainability"; return c }(), true},
		{"low test with code", func() model.LlmComment { c := base; c.Category = "test"; return c }(), true},
		{"low documentation with code", func() model.LlmComment { c := base; c.Category = "documentation"; return c }(), true},
		{"empty suggestion_code", func() model.LlmComment { c := base; c.SuggestionCode = ""; return c }(), false},
		{"whitespace suggestion_code", func() model.LlmComment { c := base; c.SuggestionCode = "   "; return c }(), false},
		{"severity high", func() model.LlmComment { c := base; c.Severity = "high"; return c }(), false},
		{"severity critical", func() model.LlmComment { c := base; c.Severity = "critical"; return c }(), false},
		{"category bug", func() model.LlmComment { c := base; c.Category = "bug"; return c }(), false},
		{"category security", func() model.LlmComment { c := base; c.Category = "security"; return c }(), false},
		{"mixed case severity + category", func() model.LlmComment {
			c := base
			c.Severity = "LOW"
			c.Category = "Style"
			return c
		}(), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSuggestion(tc.in); got != tc.want {
				t.Errorf("IsSuggestion(%+v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFilterBlocking(t *testing.T) {
	suggestion := model.LlmComment{Severity: "low", Category: "style", SuggestionCode: "x := 1", Path: "a.go"}
	blocker := model.LlmComment{Severity: "critical", Category: "security", Path: "b.go"}
	mediumBug := model.LlmComment{Severity: "medium", Category: "bug", Path: "c.go"}
	lowBug := model.LlmComment{Severity: "low", Category: "bug", SuggestionCode: "y := 2", Path: "d.go"}

	in := []model.LlmComment{suggestion, blocker, mediumBug, lowBug}

	t.Run("blocking=false drops suggestions", func(t *testing.T) {
		got := FilterBlocking(in, false)
		want := []model.LlmComment{blocker, mediumBug, lowBug}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("blocking=true returns everything", func(t *testing.T) {
		got := FilterBlocking(in, true)
		if !reflect.DeepEqual(got, in) {
			t.Errorf("got %+v, want %+v", got, in)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := FilterBlocking(nil, false)
		if len(got) != 0 {
			t.Errorf("got %+v, want empty", got)
		}
	})
}
