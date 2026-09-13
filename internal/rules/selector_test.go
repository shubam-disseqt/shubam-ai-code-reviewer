// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package rules

import (
	"testing"
)

func ruleT(id, scope string, repos, paths, exclude []string, enabled bool) Rule {
	e := enabled
	return Rule{
		ID:           id,
		Title:        id,
		Body:         "body",
		Scope:        scope,
		Repos:        repos,
		Paths:        paths,
		ExcludePaths: exclude,
		Enabled:      &e,
	}
}

func TestSelect(t *testing.T) {
	global := ruleT("z-global", "global", nil, nil, nil, true)
	repoOnly := ruleT("m-repo", "repo", []string{"acme/api"}, nil, nil, true)
	repoScoped := ruleT("l-repo-path", "repo", []string{"acme/api"}, []string{"src/**/*.go"}, []string{"**/*_test.go"}, true)
	pathOnly := ruleT("a-path", "path", nil, []string{"docs/**/*.md"}, nil, true)
	disabled := ruleT("b-disabled", "global", nil, nil, nil, false)
	otherRepo := ruleT("c-otherrepo", "repo", []string{"other/repo"}, nil, nil, true)

	all := []Rule{global, repoOnly, repoScoped, pathOnly, disabled, otherRepo}

	cases := []struct {
		name string
		in   SelectionInput
		want []string // rule IDs in expected order
	}{
		{
			name: "global always kept, disabled dropped, mismatched repo dropped",
			in: SelectionInput{
				Owner: "acme", Repo: "api",
				FilePaths: []string{"src/foo/bar.go"},
			},
			// global first, then alphabetical
			want: []string{"z-global", "l-repo-path", "m-repo"},
		},
		{
			name: "repo scoped with excluded path — repoScoped drops out",
			in: SelectionInput{
				Owner: "acme", Repo: "api",
				FilePaths: []string{"src/foo/bar_test.go"},
			},
			want: []string{"z-global", "m-repo"},
		},
		{
			name: "path-only rule matches docs",
			in: SelectionInput{
				Owner: "acme", Repo: "api",
				FilePaths: []string{"docs/x.md"},
			},
			want: []string{"z-global", "a-path", "m-repo"},
		},
		{
			name: "no repo match — only global kept",
			in: SelectionInput{
				Owner: "solo", Repo: "app",
				FilePaths: []string{"src/foo.go"},
			},
			want: []string{"z-global"},
		},
		{
			name: "empty input",
			in:   SelectionInput{},
			want: []string{"z-global"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Select(all, tc.in)
			gotIDs := make([]string, len(got))
			for i, r := range got {
				gotIDs[i] = r.ID
			}
			if !equalSlices(gotIDs, tc.want) {
				t.Fatalf("Select ids:\n  want: %v\n  got:  %v", tc.want, gotIDs)
			}
		})
	}
}

func TestSelect_DeterministicOrder(t *testing.T) {
	// Two rules with the same scope: alphabetical by ID.
	a := ruleT("banana", "global", nil, nil, nil, true)
	b := ruleT("apple", "global", nil, nil, nil, true)
	got := Select([]Rule{a, b}, SelectionInput{})
	if got[0].ID != "apple" || got[1].ID != "banana" {
		t.Fatalf("expected alphabetical order, got %v then %v", got[0].ID, got[1].ID)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
