// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package rules

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// execLookPath is a package-scoped alias so the git-availability skip can be
// swapped for tests without importing os/exec at multiple call sites.
var execLookPath = exec.LookPath

// gitInit initialises a fresh git repo in dir and commits everything under it.
// Used to spin up a local remote for syncRepo tests.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	// -b main so the default branch is deterministic across git versions.
	steps := [][]string{
		{"init", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "add", "."},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init"},
	}
	for _, args := range steps {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func writeYAML(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestLoadFromDir(t *testing.T) {
	root := t.TempDir()
	rulesDir := filepath.Join(root, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeYAML(t, rulesDir, "a.yaml", `
id: a
title: Rule A
body: body A
scope: global
`)
	writeYAML(t, rulesDir, "b.yml", `
id: b
title: Rule B
body: body B
scope: path
paths: ["src/**/*.ts"]
`)
	writeYAML(t, rulesDir, "c.yaml", `
rules:
  - id: c1
    title: C1
    body: body C1
    scope: global
  - id: c2
    title: C2
    body: body C2
    scope: global
`)
	// Ignored — wrong extension.
	writeYAML(t, rulesDir, "notes.txt", "id: nope\n")

	l := NewLoader("")
	got, err := l.LoadFromDir(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 rules, got %d: %+v", len(got), got)
	}
	// Deterministic order via file sort: a, b, c1, c2.
	wantIDs := []string{"a", "b", "c1", "c2"}
	for i, r := range got {
		if r.ID != wantIDs[i] {
			t.Errorf("rule[%d].ID = %q, want %q", i, r.ID, wantIDs[i])
		}
		if r.SourcePath == "" {
			t.Errorf("rule[%d] missing SourcePath", i)
		}
	}
}

func TestLoadFromDir_MissingDir(t *testing.T) {
	l := NewLoader("")
	_, err := l.LoadFromDir(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing rules/ subdir")
	}
	if !strings.Contains(err.Error(), "cannot stat") {
		t.Errorf("unexpected err: %v", err)
	}
}

func TestLoadFromDir_NotADir(t *testing.T) {
	root := t.TempDir()
	// Make "rules" a file, not a dir.
	if err := os.WriteFile(filepath.Join(root, "rules"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLoader("")
	_, err := l.LoadFromDir(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected not-a-directory error, got %v", err)
	}
}

func TestLoadFromDir_PropagatesParseError(t *testing.T) {
	root := t.TempDir()
	rulesDir := filepath.Join(root, "rules")
	_ = os.MkdirAll(rulesDir, 0o755)
	writeYAML(t, rulesDir, "bad.yaml", "id: x\ntitle: X\nbody: Y\nscope: nope\n")

	l := NewLoader("")
	_, err := l.LoadFromDir(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "invalid scope") {
		t.Fatalf("expected invalid-scope error, got %v", err)
	}
}

func TestLoadFromRepo_LocalPath(t *testing.T) {
	root := t.TempDir()
	rulesDir := filepath.Join(root, "rules")
	_ = os.MkdirAll(rulesDir, 0o755)
	writeYAML(t, rulesDir, "a.yaml", "id: a\ntitle: A\nbody: b\nscope: global\n")

	l := NewLoader("")

	// Absolute path form.
	got, err := l.LoadFromRepo(context.Background(), root)
	if err != nil {
		t.Fatalf("LoadFromRepo(abs): %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("unexpected: %+v", got)
	}

	// file:// form.
	got2, err := l.LoadFromRepo(context.Background(), "file://"+root)
	if err != nil {
		t.Fatalf("LoadFromRepo(file://): %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("want 1, got %d", len(got2))
	}
}

func TestLoadFromRepo_Errors(t *testing.T) {
	l := NewLoader("")

	if _, err := l.LoadFromRepo(context.Background(), ""); err == nil {
		t.Error("empty spec should error")
	}
	if _, err := l.LoadFromRepo(context.Background(), "invalid-spec"); err == nil {
		t.Error("non-owner/repo spec should error")
	}
	if _, err := l.LoadFromRepo(context.Background(), "acme/api"); err == nil {
		t.Error("git spec without cache dir should error")
	}
}

func TestParseRepoSpec(t *testing.T) {
	cases := []struct {
		in               string
		owner, repo, ref string
		wantErr          bool
	}{
		{"owner/repo", "owner", "repo", "", false},
		{"owner/repo@main", "owner", "repo", "main", false},
		{"owner/repo@feature/x", "owner", "repo", "feature/x", false},
		{"noslash", "", "", "", true},
		{"/norepo", "", "", "", true},
		{"noowner/", "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			o, r, ref, err := parseRepoSpec(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if o != tc.owner || r != tc.repo || ref != tc.ref {
				t.Errorf("got (%q,%q,%q), want (%q,%q,%q)", o, r, ref, tc.owner, tc.repo, tc.ref)
			}
		})
	}
}

// TestLoadFromRepo_Git spins up a local bare git repo, points repoURLBuilder
// at it, and exercises the clone + fetch/reset paths of syncRepo.
func TestLoadFromRepo_Git(t *testing.T) {
	if _, err := execLookPath("git"); err != nil {
		t.Skip("git not available on PATH")
	}

	// Build a source repo: rules/a.yaml → single rule.
	src := t.TempDir()
	rulesDir := filepath.Join(src, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeYAML(t, rulesDir, "a.yaml", "id: a\ntitle: A\nbody: b\nscope: global\n")
	gitInit(t, src)

	// Point URL builder at the local repo. Restore afterwards.
	orig := repoURLBuilder
	repoURLBuilder = func(_, _ string) string { return src }
	defer func() { repoURLBuilder = orig }()

	cache := t.TempDir()
	l := NewLoader(cache)

	got, err := l.LoadFromRepo(context.Background(), "acme/rules@main")
	if err != nil {
		t.Fatalf("first LoadFromRepo (clone): %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("unexpected rules after clone: %+v", got)
	}

	// Second call should hit the fetch+reset branch.
	got2, err := l.LoadFromRepo(context.Background(), "acme/rules@main")
	if err != nil {
		t.Fatalf("second LoadFromRepo (fetch/reset): %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("unexpected rules after fetch: %+v", got2)
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv(refEnvVar, "custom-ref")
	if got := envOr(refEnvVar, "main"); got != "custom-ref" {
		t.Errorf("want custom-ref, got %s", got)
	}
	t.Setenv(refEnvVar, "")
	if got := envOr(refEnvVar, "main"); got != "main" {
		t.Errorf("want fallback main, got %s", got)
	}
}
