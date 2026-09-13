// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package rules

import (
	"strings"
	"testing"
)

func TestParse_SingleDoc(t *testing.T) {
	src := []byte(`
id: no-console-log
title: No console.log in prod
body: |
  Flag any console.log left in application code.
scope: repo
repos: ["disseqt/z-frontend"]
severity: warning
category: maintainability
`)
	got, err := Parse(src, "single.yaml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 rule, got %d", len(got))
	}
	r := got[0]
	if r.ID != "no-console-log" || r.Title != "No console.log in prod" {
		t.Errorf("unexpected fields: %+v", r)
	}
	if !r.IsEnabled() {
		t.Errorf("Enabled should default to true when absent")
	}
	if r.SourcePath != "single.yaml" {
		t.Errorf("SourcePath: got %q", r.SourcePath)
	}
}

func TestParse_MultiDoc(t *testing.T) {
	src := []byte(`
rules:
  - id: a
    title: Rule A
    body: body A
    scope: global
  - id: b
    title: Rule B
    body: body B
    scope: path
    paths: ["src/**/*.ts"]
    enabled: false
`)
	got, err := Parse(src, "multi.yaml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rules, got %d", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("order/ids: %+v", got)
	}
	if got[1].IsEnabled() {
		t.Errorf("rule b should be disabled")
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "empty",
			src:  "",
			want: "empty file",
		},
		{
			name: "no rule content",
			src:  "# just a comment\n",
			want: "contains no rule",
		},
		{
			name: "missing id",
			src:  "title: X\nbody: Y\nscope: global\n",
			want: "missing required field: id",
		},
		{
			name: "missing title",
			src:  "id: x\nbody: Y\nscope: global\n",
			want: "missing required field: title",
		},
		{
			name: "missing body",
			src:  "id: x\ntitle: X\nscope: global\n",
			want: "missing required field: body",
		},
		{
			name: "missing scope",
			src:  "id: x\ntitle: X\nbody: Y\n",
			want: "missing required field: scope",
		},
		{
			name: "invalid scope",
			src:  "id: x\ntitle: X\nbody: Y\nscope: sideways\n",
			want: "invalid scope",
		},
		{
			name: "invalid severity",
			src:  "id: x\ntitle: X\nbody: Y\nscope: global\nseverity: catastrophic\n",
			want: "invalid severity",
		},
		{
			name: "repo scope needs repos",
			src:  "id: x\ntitle: X\nbody: Y\nscope: repo\n",
			want: "scope=repo requires",
		},
		{
			name: "path scope needs paths",
			src:  "id: x\ntitle: X\nbody: Y\nscope: path\n",
			want: "scope=path requires",
		},
		{
			name: "malformed yaml",
			src:  "id: x\n title: [unclosed\n",
			want: "parse",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src), tc.name+".yaml")
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error mismatch:\n  want substring: %q\n  got: %v", tc.want, err)
			}
		})
	}
}

func TestParse_ExplicitEnabledFalse(t *testing.T) {
	src := []byte(`id: x
title: X
body: Y
scope: global
enabled: false
`)
	got, err := Parse(src, "x.yaml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got[0].IsEnabled() {
		t.Errorf("explicit false should be respected")
	}
}
