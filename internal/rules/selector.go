// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package rules loads org-level review rules from a git-hosted rules
// repo, filters them by scope + path glob, and renders the injectable
// "## Custom Review Rules" block for the review prompt.
package rules

import "sort"

// SelectionInput describes the review context used to filter rules.
type SelectionInput struct {
	Owner     string
	Repo      string
	FilePaths []string
}

// Select returns the subset of rules that apply to the given review input.
//
// Algorithm:
//  1. Drop disabled rules.
//  2. Keep global rules unconditionally.
//  3. Keep repo-scoped rules whose Repos list contains "owner/repo". If
//     the rule also declares Paths, at least one FilePath must match.
//  4. Keep path-scoped rules where at least one FilePath matches Paths
//     and does not match ExcludePaths.
//  5. Sort deterministically: global first, then by ID ascending.
func Select(rules []Rule, in SelectionInput) []Rule {
	out := make([]Rule, 0, len(rules))
	repoKey := in.Owner + "/" + in.Repo

	for _, r := range rules {
		if !r.IsEnabled() {
			continue
		}
		if !keep(r, in, repoKey) {
			continue
		}
		out = append(out, r)
	}

	sort.SliceStable(out, func(i, j int) bool {
		gi := out[i].Scope == "global"
		gj := out[j].Scope == "global"
		if gi != gj {
			return gi // globals first
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func keep(r Rule, in SelectionInput, repoKey string) bool {
	switch r.Scope {
	case "global":
		return true
	case "repo":
		if !contains(r.Repos, repoKey) {
			return false
		}
		// If Paths is set, treat it as an additional filter.
		if len(r.Paths) > 0 {
			return anyPathMatches(in.FilePaths, r.Paths, r.ExcludePaths)
		}
		return true
	case "path":
		return anyPathMatches(in.FilePaths, r.Paths, r.ExcludePaths)
	}
	return false
}

func anyPathMatches(paths, include, exclude []string) bool {
	for _, p := range paths {
		if !MatchAny(include, p) {
			continue
		}
		if MatchAny(exclude, p) {
			continue
		}
		return true
	}
	return false
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
