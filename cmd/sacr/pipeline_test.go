// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/scoring"
)

func TestFilterResolvedDropsUnlocated(t *testing.T) {
	in := []model.LlmComment{
		{Path: "a.go", StartLine: 1, EndLine: 2, Content: "x"},
		{Path: "b.go", Content: "unresolved"},
		{Path: "c.go", StartLine: 5, EndLine: 5, Content: "y"},
	}
	got := filterResolved(in)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d: %+v", len(got), got)
	}
	if got[0].Path != "a.go" || got[1].Path != "c.go" {
		t.Errorf("wrong survivors: %+v", got)
	}
}

func TestOwnerRepoFromEnv(t *testing.T) {
	tests := []struct {
		env, wantO, wantR string
	}{
		{"foo/bar", "foo", "bar"},
		{"foo/bar/baz", "foo", "bar/baz"},
		{"", "", ""},
		{"noslash", "", ""},
	}
	for _, tt := range tests {
		t.Setenv("GITHUB_REPOSITORY", tt.env)
		o, r := ownerRepoFromEnv()
		if o != tt.wantO || r != tt.wantR {
			t.Errorf("env=%q got (%q,%q) want (%q,%q)", tt.env, o, r, tt.wantO, tt.wantR)
		}
	}
}

func TestResolveRulesSourcePrefersOverride(t *testing.T) {
	t.Setenv("SACR_ORG_RULES_REPO", "envspec")
	if got := resolveRulesSource(""); got != "envspec" {
		t.Errorf("env: got %q", got)
	}
	if got := resolveRulesSource("override"); got != "override" {
		t.Errorf("override: got %q", got)
	}
}

func TestResolveRulesSourceEmpty(t *testing.T) {
	t.Setenv("SACR_ORG_RULES_REPO", "")
	if got := resolveRulesSource(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRulesCacheDirNonEmpty(t *testing.T) {
	if rulesCacheDir() == "" {
		t.Error("rulesCacheDir must not be empty")
	}
}

// With SACR_DB_URL unset the index still opens, at the per-repo default
// under $HOME/.sacr/index/. There is no JIT-only mode.
func TestOpenStoreNoDSNUsesDefault(t *testing.T) {
	t.Setenv("SACR_DB_URL", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	store, err := openStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	defer store.Close()
	if err := store.Ping(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".sacr", "index", "*.db"))
	if len(matches) != 1 {
		t.Errorf("expected one db under ~/.sacr/index, got %v", matches)
	}
}

func TestOpenStoreMemory(t *testing.T) {
	t.Setenv("SACR_DB_URL", "sqlite:///:memory:")
	store, err := openStore(context.Background(), ".")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if store == nil {
		t.Fatal("nil store")
	}
	defer store.Close()
	if err := store.Ping(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
}

func TestOpenStoreBadDSN(t *testing.T) {
	t.Setenv("SACR_DB_URL", "mysql://foo")
	if _, err := openStore(context.Background(), "."); err == nil {
		t.Error("expected error for unsupported DSN scheme")
	}
}

func TestNewSessionCreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SACR_SESSION_DIR", dir)
	s, err := newSession("")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()
	if s.SessionID() == "" {
		t.Error("session ID should be non-empty")
	}
}

func TestApplySelectorFiltersDeletedBinariesAndEmpty(t *testing.T) {
	diffs := []model.Diff{
		{NewPath: "a.go", Diff: "diff a"},
		{NewPath: "b.png", IsBinary: true, Diff: "binary"},
	}
	decisions := applySelector(diffs)
	if len(decisions) != 2 {
		t.Fatalf("want 2 decisions, got %d", len(decisions))
	}
	if !decisions[0].Included {
		t.Errorf(".go must be included, got %+v", decisions[0])
	}
	if decisions[1].Included {
		t.Errorf("binary must be excluded, got %+v", decisions[1])
	}
}

func TestNewGithubClientNoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	c, err := newGithubClient()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if c != nil {
		t.Error("expected nil client when GITHUB_TOKEN unset")
	}
}

func TestResolveDiffsWorkspaceMode(t *testing.T) {
	// Init an empty git repo and confirm workspace mode returns nothing.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	opts := &reviewOpts{Repo: dir, Format: "stdout"}
	diffs, err := resolveDiffs(context.Background(), opts)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected 0 diffs in empty repo, got %d", len(diffs))
	}
}

func TestBuildContextEmptyKept(t *testing.T) {
	got, err := buildContext(context.Background(), filepath.Join(t.TempDir()), nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Empty inputs → empty output, no crash.
	if got != "" {
		t.Errorf("expected empty context, got %q", got)
	}
}

func TestBlockerExitError(t *testing.T) {
	e := &blockerExitError{code: 3}
	if e.Error() == "" {
		t.Error("Error() should be non-empty")
	}
}

func TestFilterByScoreDropsSuppressAndBelowMin(t *testing.T) {
	pol, err := scoring.LoadPolicy("")
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	in := []model.LlmComment{
		// hardcoded-secret → CRITICAL, survives
		{Path: "a.go", StartLine: 1, EndLine: 1, Category: "security.hardcoded-secret", Severity: "high"},
		// naming → SUPPRESS, always dropped
		{Path: "b.go", StartLine: 2, EndLine: 2, Category: "maintainability.naming", Severity: "low"},
		// style → SUPPRESS (default), always dropped
		{Path: "c.go", StartLine: 3, EndLine: 3, Category: "style"},
		// bug default → MEDIUM, survives at min=MEDIUM
		{Path: "d.go", StartLine: 4, EndLine: 4, Category: "bug", Severity: "medium"},
	}
	got, scores := filterByScore(in, pol, scoring.SeverityMedium)
	if len(got) != 2 {
		t.Fatalf("want 2 survivors, got %d: %+v", len(got), got)
	}
	if got[0].Path != "a.go" || got[1].Path != "d.go" {
		t.Errorf("wrong survivors: %+v", got)
	}
	if len(scores) != 2 {
		t.Errorf("scores map size: got %d want 2", len(scores))
	}
	if scores[commentKey(got[0])].Severity != scoring.SeverityCritical {
		t.Errorf("first survivor severity: %+v", scores[commentKey(got[0])])
	}
}

func TestFilterByScoreMinHigh(t *testing.T) {
	pol, err := scoring.LoadPolicy("")
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	in := []model.LlmComment{
		{Path: "a.go", StartLine: 1, EndLine: 1, Category: "bug", Severity: "medium"},        // MEDIUM
		{Path: "b.go", StartLine: 2, EndLine: 2, Category: "security", Severity: "critical"}, // CRITICAL
	}
	got, _ := filterByScore(in, pol, scoring.SeverityHigh)
	if len(got) != 1 || got[0].Path != "b.go" {
		t.Fatalf("min=HIGH should keep only CRITICAL, got: %+v", got)
	}
}

func TestFilterByScoreEmpty(t *testing.T) {
	pol, _ := scoring.LoadPolicy("")
	got, scores := filterByScore(nil, pol, scoring.SeverityMedium)
	if len(got) != 0 || scores != nil {
		t.Errorf("expected empty result, got %+v / %+v", got, scores)
	}
}

func TestExitCodeUsesScoredSeverity(t *testing.T) {
	// LLM raw severity is "high" (would not exit 3), but scored is CRITICAL:
	// scoring wins.
	c := model.LlmComment{Path: "a.go", StartLine: 1, EndLine: 1, Severity: "high"}
	scores := map[string]scoring.Score{
		commentKey(c): {Severity: scoring.SeverityCritical},
	}
	if got := exitCodeForComments([]model.LlmComment{c}, scores, false); got != 3 {
		t.Errorf("scored CRITICAL should exit 3, got %d", got)
	}
}
