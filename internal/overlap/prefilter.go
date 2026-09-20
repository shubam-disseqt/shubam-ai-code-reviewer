// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/core/overlap.py
// under Apache License 2.0.

package overlap

import (
	"sort"
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/gh"
)

// jaccard is word-level Jaccard similarity, lowercased, split on whitespace.
// Returns 0.0 if either side is empty — matches Mira's noise_filter._jaccard_similarity.
func jaccard(a, b string) float64 {
	wa := wordSet(a)
	wb := wordSet(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0.0
	}
	inter := 0
	for w := range wa {
		if _, ok := wb[w]; ok {
			inter++
		}
	}
	union := len(wa) + len(wb) - inter
	if union == 0 {
		return 0.0
	}
	return float64(inter) / float64(union)
}

func wordSet(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if w != "" {
			out[w] = struct{}{}
		}
	}
	return out
}

// prefilter decides cheaply whether candidate is worth an LLM judgment.
// Returns (keep, sharedFiles). A candidate is kept when files or symbols
// intersect, or the titles are lexically similar enough. Sharing the shared
// files here saves a recompute in the renderer.
func prefilter(current PR, candidate fingerprint, titleThreshold float64) (bool, []string) {
	shared := intersectSorted(current.Paths, candidate.Paths)
	if len(shared) > 0 {
		return true, shared
	}
	if hasIntersection(current.Symbols, candidate.Symbols) {
		return true, shared
	}
	if jaccard(current.Title, candidate.Title) >= titleThreshold {
		return true, shared
	}
	return false, shared
}

// isStacked reports whether ref is the neighbour of current in a branch
// stack (A ← B ← C). Cheap and matches Mira: name-only, no history walk.
// Only immediate neighbours are caught — a 3-deep stack A ← B ← C won't
// suppress A vs C. Fine for the common case.
func isStacked(current PR, ref gh.OpenPRRef) bool {
	if ref.HeadRef != "" && ref.HeadRef == current.BaseRef {
		return true
	}
	return ref.BaseRef != "" && ref.BaseRef == current.HeadRef
}

// intersectSorted returns sorted-unique intersection of two string slices.
func intersectSorted(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(a))
	for _, x := range a {
		set[x] = struct{}{}
	}
	seen := make(map[string]struct{})
	var out []string
	for _, x := range b {
		if _, ok := set[x]; ok {
			if _, dup := seen[x]; dup {
				continue
			}
			seen[x] = struct{}{}
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func hasIntersection(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, x := range a {
		set[x] = struct{}{}
	}
	for _, x := range b {
		if _, ok := set[x]; ok {
			return true
		}
	}
	return false
}

// normalizeLogin strips the "[bot]" suffix GitHub adds to app logins and
// lowercases — dependabot[bot] and Dependabot both compare equal. Matches
// mira.providers.github._normalize_login.
func normalizeLogin(login string) string {
	return strings.TrimSuffix(strings.ToLower(login), "[bot]")
}
