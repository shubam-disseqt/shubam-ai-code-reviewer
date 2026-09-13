// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package overlap

import (
	"strings"
	"testing"
)

func TestRender_MatchesAgent2Shape(t *testing.T) {
	findings := []Finding{
		{
			Number:      412,
			HTMLURL:     "https://github.com/o/r/pull/412",
			Kind:        "merge_conflict",
			Reason:      "Both refactor `AuthService.login` and rename the same private helpers",
			SharedFiles: []string{"src/auth/login.ts", "src/auth/session.ts"},
		},
		{
			Number:  418,
			HTMLURL: "https://github.com/o/r/pull/418",
			Kind:    "duplicate_effort",
			Reason:  "Ships the same OAuth callback fix; approach differs slightly.",
		},
		{
			Number:      421,
			HTMLURL:     "https://github.com/o/r/pull/421",
			Kind:        "both",
			Reason:      "Adds rate limiting to the same endpoint",
			SharedFiles: []string{"src/auth/limits.ts", "src/auth/x.ts", "src/auth/y.ts"},
		},
	}
	got := Render(findings)
	want := `> **⚠️ Potential overlap with other open PRs** — these may be stepping on this one:
>
> - [#412](https://github.com/o/r/pull/412) (merge-conflict risk) — Both refactor ` + "`AuthService.login`" + ` and rename the same private helpers. Shared: src/auth/login.ts, src/auth/session.ts
> - [#418](https://github.com/o/r/pull/418) (duplicate effort) — Ships the same OAuth callback fix; approach differs slightly.
> - [#421](https://github.com/o/r/pull/421) (duplicate effort + merge-conflict risk) — Adds rate limiting to the same endpoint. Shared: src/auth/limits.ts, src/auth/x.ts, src/auth/y.ts
`
	if got != want {
		t.Errorf("Render mismatch:\n--got--\n%s\n--want--\n%s", got, want)
	}
}

func TestRender_SharedFilesTruncation(t *testing.T) {
	f := []Finding{{
		Number:      1,
		HTMLURL:     "u",
		Kind:        "merge_conflict",
		Reason:      "reason",
		SharedFiles: []string{"a", "b", "c", "d", "e"},
	}}
	got := Render(f)
	if !strings.Contains(got, "Shared: a, b, c +2 more") {
		t.Errorf("want '+2 more' suffix, got:\n%s", got)
	}
}

func TestRender_Empty(t *testing.T) {
	if Render(nil) != "" {
		t.Errorf("want empty, got %q", Render(nil))
	}
	if Render([]Finding{}) != "" {
		t.Errorf("want empty, got %q", Render([]Finding{}))
	}
}

func TestRender_UnknownKindFallsThrough(t *testing.T) {
	f := []Finding{{Number: 1, HTMLURL: "u", Kind: "weird", Reason: "r"}}
	got := Render(f)
	if !strings.Contains(got, "(weird)") {
		t.Errorf("want raw kind in output, got %q", got)
	}
}

func TestEnsureTrailingPeriod(t *testing.T) {
	cases := map[string]string{
		"already.":     "already.",
		"question?":    "question?",
		"exclaim!":     "exclaim!",
		"needs one":    "needs one.",
		" whitespace ": "whitespace.",
		"":             "",
	}
	for in, want := range cases {
		if got := ensureTrailingPeriod(in); got != want {
			t.Errorf("ensureTrailingPeriod(%q) = %q, want %q", in, got, want)
		}
	}
}
