// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package fingerprint

import "testing"

// base is the reference input all comparison tests deviate from.
func base() Input {
	return Input{
		Owner:    "disseqt",
		Repo:     "shubam-ai-code-reviewer",
		Category: "bug",
		Path:     "internal/foo/foo.go",
		Symbol:   "Handle",
		Snippet:  "if err != nil {\n    return err\n}",
	}
}

func TestFingerprintDeterministic(t *testing.T) {
	if Fingerprint(base()) != Fingerprint(base()) {
		t.Fatal("fingerprint not deterministic")
	}
}

func TestFingerprintEqualityUnderNormalization(t *testing.T) {
	tests := []struct {
		name string
		mut  func(Input) Input
	}{
		{
			name: "whitespace-only diff same fingerprint",
			mut: func(i Input) Input {
				i.Snippet = "if   err  !=   nil {\n\t\treturn err\n  }"
				return i
			},
		},
		{
			name: "comment-only diff same fingerprint",
			mut: func(i Input) Input {
				i.Snippet = "if err != nil { // TODO: log this\n    return err // bubble up\n}"
				return i
			},
		},
		{
			name: "line-number prefix same fingerprint",
			mut: func(i Input) Input {
				i.Snippet = "12: if err != nil {\n13:     return err\n14: }"
				return i
			},
		},
		{
			name: "python-style hash comment same fingerprint",
			mut: func(i Input) Input {
				i.Snippet = "if err != nil { # inline note\n    return err\n}"
				return i
			},
		},
		{
			name: "extension casing same fingerprint",
			mut: func(i Input) Input {
				i.Path = "internal/foo/foo.GO"
				return i
			},
		},
		{
			name: "CRLF vs LF same fingerprint",
			mut: func(i Input) Input {
				i.Snippet = "if err != nil {\r\n    return err\r\n}"
				return i
			},
		},
		{
			name: "adding a real comment same fingerprint",
			mut: func(i Input) Input {
				i.Snippet = "if err != nil { // freshly added note\n    return err\n} // and another"
				return i
			},
		},
	}
	want := Fingerprint(base())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fingerprint(tt.mut(base()))
			if got != want {
				t.Errorf("fingerprint drifted:\n  base = %s\n  got  = %s", want, got)
			}
		})
	}
}

func TestFingerprintChangesForRealEdits(t *testing.T) {
	tests := []struct {
		name string
		mut  func(Input) Input
	}{
		{
			name: "rename changes fingerprint",
			mut: func(i Input) Input {
				i.Path = "internal/foo/bar.go"
				return i
			},
		},
		{
			name: "different symbol changes fingerprint",
			mut: func(i Input) Input {
				i.Symbol = "HandleAll"
				return i
			},
		},
		{
			name: "actual code edit changes fingerprint",
			mut: func(i Input) Input {
				i.Snippet = "if err != nil {\n    return fmt.Errorf(\"handle: %w\", err)\n}"
				return i
			},
		},
		{
			name: "different owner changes fingerprint",
			mut: func(i Input) Input {
				i.Owner = "other"
				return i
			},
		},
		{
			name: "different category changes fingerprint",
			mut: func(i Input) Input {
				i.Category = "security"
				return i
			},
		},
	}
	base := Fingerprint(base())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fingerprint(tt.mut(baseInput()))
			if got == base {
				t.Errorf("fingerprint should have changed but didn't: %s", got)
			}
		})
	}
}

// baseInput is a helper: the tests above use base() but the "changes" table
// mutates a fresh copy, so a rename to a helper reads better.
func baseInput() Input { return base() }

func TestFingerprintEmptyInputStable(t *testing.T) {
	// An empty Input still hashes deterministically. Guards against nil-panic
	// when a caller forgets a field.
	if Fingerprint(Input{}) == "" {
		t.Fatal("empty input should still produce a hash")
	}
}

func TestNormalizeSnippet(t *testing.T) {
	tests := []struct {
		name     string
		in, want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "collapse whitespace", in: "foo   bar\n baz", want: "foo bar baz"},
		{name: "strip line-number prefix", in: "12: foo\n13: bar", want: "foo bar"},
		{name: "trailing // comment", in: "foo // trailing", want: "foo"},
		{name: "trailing # comment", in: "foo # trailing", want: "foo"},
		{name: "trim outer whitespace", in: "  \t hello \t  ", want: "hello"},

		// URL scheme must survive the comment stripper.
		{name: "http url in code", in: `get("http://example.com/x")`, want: `get("http://example.com/x")`},
		{name: "https url in code", in: `get("https://example.com")`, want: `get("https://example.com")`},
		{name: "git+ssh url in code", in: `clone("git+ssh://host/repo")`, want: `clone("git+ssh://host/repo")`},

		// Bare URL outside any string literal (yaml/config/shell snippets).
		{name: "bare http url", in: `url: http://example.com/x`, want: `url: http://example.com/x`},
		{name: "bare url then real comment", in: `url: http://example.com # note`, want: `url: http://example.com`},
		{name: "uppercase scheme is not a URL", in: `foo HTTP://x // c`, want: `foo HTTP:`},

		// // inside a string literal must not be treated as a comment.
		{name: "// inside double-quoted string", in: `s := "//"`, want: `s := "//"`},
		{name: "// inside single-quoted string", in: `s = '//'`, want: `s = '//'`},
		{name: "// inside backtick raw string", in: "s := `no // comment here`", want: "s := `no // comment here`"},
		{name: "// inside triple-quoted string", in: `s = """no // here"""`, want: `s = """no // here"""`},
		{name: "# inside string literal", in: `s := "#not a comment"`, want: `s := "#not a comment"`},

		// Comment tail is stripped, code head is kept.
		{name: "code then // tail", in: `foo(x) // side note`, want: `foo(x)`},
		{name: "code then # tail", in: `foo(x) # side note`, want: `foo(x)`},
		{name: "// after closing string", in: `s := "hello" // real comment`, want: `s := "hello"`},

		// Escaped quote inside a string does not close it.
		{name: "escaped quote inside string", in: `s := "he said \"hi\"" // c`, want: `s := "he said \"hi\""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSnippet(tt.in); got != tt.want {
				t.Errorf("normalizeSnippet(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"foo.GO", "foo.go"},
		{"a/b/c.Ts", "a/b/c.ts"},
		{"README", "README"}, // no extension → untouched
		{" a.go ", "a.go"},
	}
	for _, tt := range tests {
		if got := normalizePath(tt.in); got != tt.want {
			t.Errorf("normalizePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
