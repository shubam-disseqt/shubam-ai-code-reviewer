// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGovulncheck(t *testing.T) {
	raw, err := os.ReadFile("testdata/govulncheck.ndjson")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Fixture uses REPOROOT as a placeholder — substitute the fake repo
	// path we want to match against.
	repo := "/tmp/fake-repo"
	data := bytes.ReplaceAll(raw, []byte("REPOROOT"), []byte(repo))

	findings, err := parseGovulncheck(data, repo)
	if err != nil {
		t.Fatalf("parseGovulncheck: %v", err)
	}

	// The fixture has two vulns, but only the first one traces into
	// user code (repoRoot). The second lives entirely in a module cache
	// path and must be filtered out.
	if len(findings) != 1 {
		t.Fatalf("want 1 user-code finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Tool != "govulncheck" {
		t.Errorf("Tool: got %q", f.Tool)
	}
	if f.RuleID != "GO-2024-2947" {
		t.Errorf("RuleID: got %q", f.RuleID)
	}
	if f.Kind != KindCVE {
		t.Errorf("Kind: got %q", f.Kind)
	}
	if f.Severity != SeverityHigh {
		t.Errorf("Severity: got %q", f.Severity)
	}
	if !strings.HasSuffix(f.Path, "internal/gh/client.go") {
		t.Errorf("Path did not land on user code: %q", f.Path)
	}
	if f.Line != 57 {
		t.Errorf("Line: got %d", f.Line)
	}
	if !strings.Contains(f.Description, "x/net") {
		t.Errorf("Description lost OSV summary: %q", f.Description)
	}
}

func TestParseGovulncheckSkipsBadLines(t *testing.T) {
	input := []byte(`{"osv":{"id":"GO-1","summary":"a"}}` + "\n" +
		"not-json-at-all\n" +
		`{"finding":{"osv":"GO-1","trace":[{"module":"m","package":"p","function":"f","position":{"filename":"/repo/x.go","line":10}}]}}` + "\n" +
		"\n")
	findings, err := parseGovulncheck(input, "/repo")
	if err != nil {
		t.Fatalf("parseGovulncheck: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 finding despite bad lines, got %d", len(findings))
	}
	if findings[0].Line != 10 {
		t.Errorf("Line: got %d", findings[0].Line)
	}
}

func TestGovulncheckScannerName(t *testing.T) {
	s := &govulncheckScanner{}
	if s.Name() != "govulncheck" {
		t.Errorf("Name: got %q", s.Name())
	}
}

// Real govulncheck output: user frames carry repo-relative filenames and
// the module path from go.mod; stdlib frames are "src/...". Before the
// module-path match every finding was dropped.
func TestParseGovulncheckRealOutputRelativeUserFrames(t *testing.T) {
	raw, err := os.ReadFile("testdata/govulncheck_real.ndjson")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/shubam-disseqt/zreview-e2e-matrix\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	findings, err := parseGovulncheck(raw, repo)
	if err != nil {
		t.Fatalf("parseGovulncheck: %v", err)
	}

	// Two findings for the same OSV: one traces into internal/httpclient,
	// the other is a module-level record with no positions.
	if len(findings) != 1 {
		t.Fatalf("want 1 user-code finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Path != "internal/httpclient/fetch.go" || f.Line != 13 {
		t.Errorf("want internal/httpclient/fetch.go:13, got %s:%d", f.Path, f.Line)
	}
	if f.RuleID != "GO-2026-6218" {
		t.Errorf("RuleID: got %q", f.RuleID)
	}
	if !strings.Contains(f.Evidence, "FetchAll") {
		t.Errorf("Evidence should name the user function, got %q", f.Evidence)
	}
}

func TestReadModulePath(t *testing.T) {
	repo := t.TempDir()
	if got := readModulePath(repo); got != "" {
		t.Errorf("no go.mod: want empty, got %q", got)
	}
	_ = os.WriteFile(filepath.Join(repo, "go.mod"), []byte("// header\nmodule example.com/x/y\n"), 0o644)
	if got := readModulePath(repo); got != "example.com/x/y" {
		t.Errorf("got %q", got)
	}
}

// Several traces of one vuln reaching the same call site collapse to one finding.
func TestParseGovulncheckDedupesSameSite(t *testing.T) {
	rec := `{"finding":{"osv":"GO-9","trace":[{"module":"stdlib","function":"A","position":{"filename":"src/a.go","line":1}},{"module":"m","package":"p","function":"f","position":{"filename":"x.go","line":10}}]}}`
	input := []byte(rec + "\n" + rec + "\n")
	repo := t.TempDir()
	_ = os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module m\n"), 0o644)
	findings, err := parseGovulncheck(input, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 deduped finding, got %d", len(findings))
	}
}
