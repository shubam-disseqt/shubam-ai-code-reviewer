// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package overlap

import (
	"reflect"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/gh"
)

func TestJaccard(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want float64
	}{
		{"identical", "add rate limit", "add rate limit", 1.0},
		{"disjoint", "auth login", "readme typo", 0.0},
		{"empty-a", "", "anything", 0.0},
		{"empty-b", "anything", "", 0.0},
		{"case-insensitive", "Add Login", "add LOGIN", 1.0},
		// {"add","rate"} ∩ {"add","limit"} = {"add"} size 1 / union 3 = 0.333…
		{"partial", "add rate", "add limit", 1.0 / 3.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := jaccard(tc.a, tc.b)
			if !approxEq(got, tc.want) {
				t.Errorf("jaccard(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestPrefilter_FilesOverlap(t *testing.T) {
	cur := PR{Paths: []string{"a.go", "b.go"}, Title: "x"}
	cand := fingerprint{Paths: []string{"b.go", "c.go"}, Title: "y"}
	keep, shared := prefilter(cur, cand, 0.4)
	if !keep {
		t.Errorf("keep=false, want true")
	}
	if !reflect.DeepEqual(shared, []string{"b.go"}) {
		t.Errorf("shared=%v, want [b.go]", shared)
	}
}

func TestPrefilter_SymbolsOverlap(t *testing.T) {
	cur := PR{Paths: []string{"a.go"}, Symbols: []string{"Login"}, Title: "add banner"}
	cand := fingerprint{Paths: []string{"b.go"}, Symbols: []string{"Login", "Logout"}, Title: "typo"}
	keep, shared := prefilter(cur, cand, 0.4)
	if !keep {
		t.Errorf("keep=false, want true (symbol overlap)")
	}
	if len(shared) != 0 {
		t.Errorf("shared=%v, want empty (files disjoint)", shared)
	}
}

func TestPrefilter_TitleJaccard(t *testing.T) {
	cur := PR{Title: "add rate limiting to auth endpoint"}
	cand := fingerprint{Title: "add rate limiting to login endpoint"}
	keep, _ := prefilter(cur, cand, 0.4)
	if !keep {
		t.Errorf("keep=false — matching titles should pass 0.4 threshold")
	}
}

func TestPrefilter_AllBelow(t *testing.T) {
	cur := PR{Paths: []string{"a.go"}, Symbols: []string{"X"}, Title: "readme typo"}
	cand := fingerprint{Paths: []string{"b.go"}, Symbols: []string{"Y"}, Title: "new database migration"}
	keep, shared := prefilter(cur, cand, 0.4)
	if keep {
		t.Errorf("keep=true, want false")
	}
	if len(shared) != 0 {
		t.Errorf("shared=%v, want empty", shared)
	}
}

func TestIsStacked(t *testing.T) {
	cur := PR{HeadRef: "feature-x", BaseRef: "main"}
	tests := []struct {
		name string
		ref  gh.OpenPRRef
		want bool
	}{
		{"stacked-on-top", gh.OpenPRRef{BaseRef: "feature-x", HeadRef: "feature-x-b"}, true},
		{"cur-stacked-on-ref", gh.OpenPRRef{HeadRef: "main", BaseRef: "root"}, true},
		{"unrelated", gh.OpenPRRef{HeadRef: "feature-y", BaseRef: "main"}, false},
		{"empty-refs", gh.OpenPRRef{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStacked(cur, tc.ref); got != tc.want {
				t.Errorf("isStacked(%+v) = %v, want %v", tc.ref, got, tc.want)
			}
		})
	}
}

func TestNormalizeLogin(t *testing.T) {
	tests := map[string]string{
		"dependabot[bot]": "dependabot",
		"Dependabot":      "dependabot",
		"Alice":           "alice",
		"":                "",
	}
	for in, want := range tests {
		if got := normalizeLogin(in); got != want {
			t.Errorf("normalizeLogin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLooksLikeBot(t *testing.T) {
	tests := map[string]bool{
		"dependabot[bot]": true,
		"alice":           false,
		"":                false,
		"[bot]":           false, // too short — no name portion
	}
	for in, want := range tests {
		if got := looksLikeBot(in); got != want {
			t.Errorf("looksLikeBot(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIntersectSorted_Dedup(t *testing.T) {
	got := intersectSorted([]string{"a", "b", "b"}, []string{"b", "b", "c"})
	if !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("intersectSorted = %v, want [b]", got)
	}
}

func TestIntersectSorted_Empty(t *testing.T) {
	if intersectSorted(nil, []string{"a"}) != nil {
		t.Errorf("want nil for empty a")
	}
	if intersectSorted([]string{"a"}, nil) != nil {
		t.Errorf("want nil for empty b")
	}
}

func approxEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
