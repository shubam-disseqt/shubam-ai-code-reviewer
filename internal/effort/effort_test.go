// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package effort

import (
	"reflect"
	"testing"
)

// baselinePolicy loads the embedded default. Tests use this so a change to
// the shipped weights that would silently shift every real score shows up
// as a test diff first.
func baselinePolicy(t *testing.T) Policy {
	t.Helper()
	p, err := LoadPolicy(t.TempDir()) // no repo override, no env → embedded
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	return p
}

func TestCompute_EmptyInputs_YieldsBaseOnly(t *testing.T) {
	got := Compute(Inputs{}, baselinePolicy(t))
	if got.Value != 1 { // base 0.5 rounds to 1
		t.Errorf("empty inputs Value = %d, want 1 (base=0.5 rounded)", got.Value)
	}
	if got.Label != "trivial" {
		t.Errorf("label = %q, want trivial", got.Label)
	}
	if got.Dot != "🟢" {
		t.Errorf("dot = %q, want green", got.Dot)
	}
}

func TestCompute_ReproducibleAcrossRuns(t *testing.T) {
	// Same inputs + same policy MUST produce identical Scores every time.
	in := Inputs{
		Files: []FileDelta{
			{Path: "cmd/api/main.go", LinesAdded: 50, LinesDeleted: 5},
			{Path: "internal/auth/token.go", LinesAdded: 30, LinesDeleted: 0, IsNew: true},
		},
		FindingsBySev:  map[string]int{"HIGH": 2, "MEDIUM": 1},
		OverlappingPRs: 1,
	}
	p := baselinePolicy(t)
	a := Compute(in, p)
	b := Compute(in, p)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("reproducibility broken:\n a=%+v\n b=%+v", a, b)
	}
}

func TestCompute_ClampsAt10(t *testing.T) {
	// Enormous diff + lots of findings should be clamped, not overflow.
	files := make([]FileDelta, 500)
	for i := range files {
		files[i] = FileDelta{Path: "f.go", LinesAdded: 500, LinesDeleted: 500, IsNew: true}
	}
	got := Compute(Inputs{
		Files:          files,
		FindingsBySev:  map[string]int{"CRITICAL": 100, "HIGH": 100},
		OverlappingPRs: 20,
	}, baselinePolicy(t))
	if got.Value != 10 {
		t.Errorf("Value = %d, want 10 (clamped)", got.Value)
	}
	if got.Raw > 10 {
		t.Errorf("Raw = %f, want <= 10 after clamp", got.Raw)
	}
	if got.Dot != "🔴" {
		t.Errorf("dot = %q, want red at 10", got.Dot)
	}
}

func TestCompute_AuthPathContributes(t *testing.T) {
	base := Compute(Inputs{
		Files: []FileDelta{{Path: "internal/user/profile.go", LinesAdded: 20}},
	}, baselinePolicy(t))
	withAuth := Compute(Inputs{
		Files: []FileDelta{{Path: "internal/auth/token.go", LinesAdded: 20}},
	}, baselinePolicy(t))
	if withAuth.Raw <= base.Raw {
		t.Errorf("auth path should raise the score; base=%f auth=%f",
			base.Raw, withAuth.Raw)
	}
	// Contribution should be present in the audit trail.
	found := false
	for _, c := range withAuth.Contributions {
		if c.Signal == "Auth path touched" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Auth contribution missing from audit trail")
	}
}

func TestCompute_AuthNegativeCase_NoFalseMatch(t *testing.T) {
	// The classic footgun: 'authors.go' must NOT trigger the auth bucket.
	got := Compute(Inputs{
		Files: []FileDelta{
			{Path: "internal/user/authors.go", LinesAdded: 20},
			{Path: "docs/authentication.html", LinesAdded: 20},
		},
	}, baselinePolicy(t))
	for _, c := range got.Contributions {
		if c.Signal == "Auth path touched" {
			t.Errorf("false match: authors.go / authentication.html triggered auth bucket: %+v", c)
		}
	}
}

func TestCompute_MigrationPath(t *testing.T) {
	got := Compute(Inputs{
		Files: []FileDelta{{Path: "db/migrations/0042_users.sql", LinesAdded: 10}},
	}, baselinePolicy(t))
	if !hasSignal(got, "Migration path touched") {
		t.Error("expected Migration contribution")
	}
}

func TestCompute_InfraPath_Dockerfile(t *testing.T) {
	got := Compute(Inputs{
		Files: []FileDelta{{Path: "deploy/Dockerfile", LinesAdded: 5}},
	}, baselinePolicy(t))
	if !hasSignal(got, "Infra path touched") {
		t.Error("expected Infra contribution for Dockerfile")
	}
}

func TestCompute_TestRatioBonusApplies(t *testing.T) {
	// 3 code + 3 test files = 50% test ratio → BonusAt50.
	files := []FileDelta{
		{Path: "a.go", LinesAdded: 20},
		{Path: "b.go", LinesAdded: 20},
		{Path: "c.go", LinesAdded: 20},
		{Path: "a_test.go", LinesAdded: 20, IsTest: true},
		{Path: "b_test.go", LinesAdded: 20, IsTest: true},
		{Path: "c_test.go", LinesAdded: 20, IsTest: true},
	}
	got := Compute(Inputs{Files: files}, baselinePolicy(t))
	found := false
	for _, c := range got.Contributions {
		if c.Signal == "Test file ratio" {
			found = true
			if c.Points >= 0 {
				t.Errorf("test-ratio bonus should be negative, got %f", c.Points)
			}
		}
	}
	if !found {
		t.Error("Test file ratio contribution missing")
	}
}

// TestCompute_RenamedFilesNotCountedAsNew verifies that a whole-package move
// (renamed files with IsNew flag) doesn't inflate the score via the new_files
// bucket. Renamed files should still contribute to files_changed and
// loc_churn, but never to new_files.
func TestCompute_RenamedFilesNotCountedAsNew(t *testing.T) {
	renamed := []FileDelta{
		{Path: "internal/bar/a.go", LinesAdded: 1, LinesDeleted: 1, IsNew: true, IsRenamed: true},
		{Path: "internal/bar/b.go", LinesAdded: 1, LinesDeleted: 1, IsNew: true, IsRenamed: true},
		{Path: "internal/bar/c.go", LinesAdded: 1, LinesDeleted: 1, IsNew: true, IsRenamed: true},
	}
	got := Compute(Inputs{Files: renamed}, baselinePolicy(t))
	for _, c := range got.Contributions {
		if c.Signal == "New files" {
			t.Errorf("renamed files should not trigger New files contribution: %+v", c)
		}
	}
	// Sanity: LOC churn and Files changed should still fire.
	if !hasSignal(got, "Files changed") {
		t.Error("Files changed contribution missing for renamed files")
	}

	// Contrast: three truly new files DO add a new_files contribution.
	fresh := []FileDelta{
		{Path: "internal/bar/a.go", LinesAdded: 1, LinesDeleted: 1, IsNew: true},
		{Path: "internal/bar/b.go", LinesAdded: 1, LinesDeleted: 1, IsNew: true},
		{Path: "internal/bar/c.go", LinesAdded: 1, LinesDeleted: 1, IsNew: true},
	}
	freshScore := Compute(Inputs{Files: fresh}, baselinePolicy(t))
	if !hasSignal(freshScore, "New files") {
		t.Error("truly new files should still trigger New files contribution")
	}
	if freshScore.Raw <= got.Raw {
		t.Errorf("truly new files should score higher than a rename of the same count; new=%f renamed=%f",
			freshScore.Raw, got.Raw)
	}
}

func TestCompute_CapsAreEnforced(t *testing.T) {
	// LOC churn cap is 4.0 at 0.10/10 lines. 100 lines would cost 1.0.
	// 500 lines would want 5.0 but must cap.
	files := []FileDelta{{Path: "big.go", LinesAdded: 5000}}
	got := Compute(Inputs{Files: files}, baselinePolicy(t))
	for _, c := range got.Contributions {
		if c.Signal == "LOC churn" && !c.Capped {
			t.Errorf("LOC churn should be capped at 5000 lines, got %+v", c)
		}
	}
}

func TestLabelAndDot_Bands(t *testing.T) {
	tests := []struct {
		v         int
		wantLabel string
		wantDot   string
	}{
		{0, "trivial", "🟢"},
		{2, "trivial", "🟢"},
		{3, "light", "🟢"},
		{4, "light", "🟢"},
		{5, "medium", "🟡"},
		{6, "medium", "🟡"},
		{7, "medium-high", "🟡"},
		{8, "medium-high", "🟡"},
		{9, "heavy", "🔴"},
		{10, "heavy", "🔴"},
	}
	for _, tt := range tests {
		gotLabel, gotDot := labelAndDot(tt.v)
		if gotLabel != tt.wantLabel || gotDot != tt.wantDot {
			t.Errorf("labelAndDot(%d) = (%q,%q), want (%q,%q)",
				tt.v, gotLabel, gotDot, tt.wantLabel, tt.wantDot)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	yes := []string{
		"foo_test.go",
		"pkg/foo_test.go",
		"foo.test.ts",
		"src/foo.spec.js",
		"test_foo.py",
		"tests/foo.py",
		"__tests__/foo.js",
	}
	no := []string{
		"foo.go",
		"foo/bar.go",
		"testify_helper.go",
		"attest.go",
	}
	for _, p := range yes {
		if !IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if IsTestFile(p) {
			t.Errorf("IsTestFile(%q) = true, want false", p)
		}
	}
}

func TestLoadPolicy_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/custom.yaml"
	if err := writeFile(p, "base: 9.0\nfiles_changed:\n  points_per_file: 0\n  cap: 0\n"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZREVIEW_EFFORT_POLICY", p)
	pol, err := LoadPolicy(t.TempDir())
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if pol.Base != 9.0 {
		t.Errorf("expected env override to load base=9.0, got %v", pol.Base)
	}
	if !contains(pol.Source(), "env(") {
		t.Errorf("Source should indicate env: %q", pol.Source())
	}
}

func hasSignal(s Score, name string) bool {
	for _, c := range s.Contributions {
		if c.Signal == name {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func writeFile(p, content string) error {
	return writeAll(p, []byte(content))
}
