// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package selector

import (
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

func TestEffectivePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		diff model.Diff
		want string
	}{
		{"modified uses new", model.Diff{OldPath: "a.go", NewPath: "a.go"}, "a.go"},
		{"deleted uses old", model.Diff{OldPath: "a.go", NewPath: "/dev/null"}, "a.go"},
		{"empty new falls back", model.Diff{OldPath: "a.go", NewPath: ""}, "a.go"},
		{"rename uses new", model.Diff{OldPath: "a.go", NewPath: "b.go"}, "b.go"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := effectivePath(tc.diff)
			if got != tc.want {
				t.Fatalf("effectivePath = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestWhyExcluded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		diff model.Diff
		opts Options
		want ExclusionReason
	}{
		{
			name: "plain go file included",
			diff: model.Diff{NewPath: "cmd/x/main.go"},
			want: ReasonNone,
		},
		{
			name: "binary excluded first",
			diff: model.Diff{NewPath: "img.png", IsBinary: true},
			want: ReasonBinary,
		},
		{
			name: "binary beats deleted",
			diff: model.Diff{OldPath: "img.png", NewPath: "/dev/null", IsBinary: true, IsDeleted: true},
			opts: Options{ExcludeDeleted: true},
			want: ReasonBinary,
		},
		{
			name: "deleted only when configured",
			diff: model.Diff{OldPath: "a.go", NewPath: "/dev/null", IsDeleted: true},
			opts: Options{ExcludeDeleted: true},
			want: ReasonDeleted,
		},
		{
			name: "deleted kept when not configured",
			diff: model.Diff{OldPath: "a.go", NewPath: "/dev/null", IsDeleted: true},
			want: ReasonNone,
		},
		{
			name: "user exclude matches",
			diff: model.Diff{NewPath: "internal/secret.go"},
			opts: Options{ExcludePatterns: []string{"**/secret.go"}},
			want: ReasonUserExclude,
		},
		{
			name: "include allowlist miss",
			diff: model.Diff{NewPath: "cmd/x/main.go"},
			opts: Options{IncludePatterns: []string{"pkg/**"}},
			want: ReasonNotAllowed,
		},
		{
			name: "include allowlist hit short-circuits extension gate",
			// A `.txt` would normally fail the extension gate; include wins.
			diff: model.Diff{NewPath: "docs/notes.txt"},
			opts: Options{IncludePatterns: []string{"docs/**"}},
			want: ReasonNone,
		},
		{
			name: "include allowlist hit short-circuits default pattern",
			// `vendor/**` is default-skip; explicit include overrides.
			diff: model.Diff{NewPath: "vendor/foo/bar.go"},
			opts: Options{IncludePatterns: []string{"vendor/**"}},
			want: ReasonNone,
		},
		{
			name: "extension not indexable",
			diff: model.Diff{NewPath: "README.md"},
			want: ReasonExtension,
		},
		{
			name: "default pattern hit",
			diff: model.Diff{NewPath: "vendor/foo/bar.go"},
			want: ReasonDefaultPattern,
		},
		{
			name: "user exclude wins over include",
			diff: model.Diff{NewPath: "pkg/secret.go"},
			opts: Options{
				IncludePatterns: []string{"pkg/**"},
				ExcludePatterns: []string{"**/secret.go"},
			},
			want: ReasonUserExclude,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := whyExcluded(tc.diff, tc.opts)
			if got != tc.want {
				t.Fatalf("whyExcluded = %q; want %q", got, tc.want)
			}
		})
	}
}
