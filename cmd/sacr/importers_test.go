// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newGoRepo builds a committed repo: internal/currency is imported by
// cmd/api/price.go and by a sibling file in its own package.
func newGoRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                        "module example.com/m\n\ngo 1.22\n",
		"internal/currency/currency.go": "package currency\n\nfunc ToCents(s string) (int64, error) { return 0, nil }\n",
		"internal/currency/format.go":   "package currency\n\nfunc Format(c int64) string { return \"\" }\n",
		"cmd/api/price.go":              "package main\n\nimport \"example.com/m/internal/currency\"\n\nfunc price(s string) int64 {\n\tc, _ := currency.ToCents(s)\n\treturn c\n}\n",
		"cmd/api/price_test.go":         "package main\n\nimport \"example.com/m/internal/currency\"\n\nvar _ = currency.Format\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"add", "."}, {"commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestFindGoImportersReturnsUntouchedCallers(t *testing.T) {
	repo := newGoRepo(t)

	got := findGoImporters(context.Background(), repo, []string{"internal/currency/currency.go"})

	if len(got) != 1 {
		t.Fatalf("want exactly the non-test caller, got %+v", got)
	}
	im := got[0]
	if im.Path != "cmd/api/price.go" || im.Pkg != "internal/currency" {
		t.Errorf("unexpected importer %+v", im)
	}
	if len(im.Lines) != 1 || !strings.Contains(im.Lines[0], "currency.ToCents") || !strings.HasPrefix(im.Lines[0], "6: ") {
		t.Errorf("want the numbered call-site line, got %v", im.Lines)
	}
}

func TestFindGoImportersSkipsChangedFilesAndNoGoMod(t *testing.T) {
	repo := newGoRepo(t)
	if got := findGoImporters(context.Background(), repo, []string{"internal/currency/currency.go", "cmd/api/price.go"}); len(got) != 0 {
		t.Errorf("caller in the diff must not be listed, got %+v", got)
	}
	if got := findGoImporters(context.Background(), t.TempDir(), []string{"a.go"}); got != nil {
		t.Errorf("no go.mod must yield nil, got %+v", got)
	}
}

func TestRenderImporters(t *testing.T) {
	if renderImporters(nil) != "" {
		t.Error("empty input must render nothing")
	}
	out := renderImporters([]Importer{{Path: "cmd/api/price.go", Pkg: "internal/currency", Lines: []string{"6: c, _ := currency.ToCents(s)"}}})
	for _, want := range []string{"### Imported by", "`cmd/api/price.go` imports `internal/currency`", "6: c, _ := currency.ToCents(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
