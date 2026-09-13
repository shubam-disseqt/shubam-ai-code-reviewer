// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package selector

import "testing"

func TestMatchesDefaultSkip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"top-level source file", "src/main.go", false},
		{"vendored dep", "vendor/github.com/x/y/z.go", true},
		{"nested vendor", "svc/api/vendor/foo/bar.go", true},
		{"node_modules root", "node_modules/react/index.js", true},
		{"nested node_modules", "web/frontend/node_modules/pkg/a.js", true},
		{"dist bundle", "dist/app.js", true},
		{"build output", "build/main", true},
		{"lock file root", "package-lock.json", true},
		{"lock file nested", "sub/package-lock.json", true},
		{"yarn lock nested", "web/yarn.lock", true},
		{"generic *.lock", "poetry.lock", true},
		{"min.js", "public/vendor/bundle.min.js", true},
		{"min.css", "static/site.min.css", true},
		{"source map", "static/site.js.map", true},
		{"git internals", ".git/HEAD", true},
		{"nested git internals", "worktrees/x/.git/config", true},
		{"pycache", "pkg/__pycache__/mod.cpython-311.pyc", true},
		{"innocuous css", "static/site.css", false},
		{"innocuous js", "static/site.js", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := matchesDefaultSkip(tc.path)
			if got != tc.want {
				t.Fatalf("matchesDefaultSkip(%q) = %v; want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestMatchesAnyMalformedPattern(t *testing.T) {
	t.Parallel()
	// An unterminated bracket class is malformed; doublestar returns an error
	// which we swallow and report as a non-match. This is the "gate, not a
	// validator" contract.
	if matchesAny("foo/bar.go", []string{"[abc"}) {
		t.Fatal("malformed pattern should not match")
	}
}

func TestMatchesAnyEmpty(t *testing.T) {
	t.Parallel()
	if matchesAny("foo/bar.go", nil) {
		t.Fatal("nil patterns should not match")
	}
	if matchesAny("foo/bar.go", []string{}) {
		t.Fatal("empty patterns should not match")
	}
}
