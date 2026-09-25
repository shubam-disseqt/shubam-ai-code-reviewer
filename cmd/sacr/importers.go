// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/gitcmd"
)

const (
	maxImporters       = 20
	maxImporterExcerpt = 8
)

// Importer is a file outside the diff that imports a package the diff
// changed. Lines holds the importer's source lines that reference the
// package identifier, prefixed with their line number.
type Importer struct {
	Path  string
	Pkg   string
	Lines []string
}

// findGoImporters returns files that import any package containing a
// changed Go file, excluding the changed files and the package's own
// files. Deterministic: go.mod module path + `git grep` on the exact import
// string, no LLM. Errors degrade to an empty result.
//
// ponytail: Go only. TS/Python importers need per-language path resolution;
// add when a non-Go repo needs blast radius.
func findGoImporters(ctx context.Context, repo string, changed []string) []Importer {
	mod := goModulePath(repo)
	if mod == "" {
		return nil
	}
	changedSet := make(map[string]struct{}, len(changed))
	dirs := map[string]struct{}{}
	for _, p := range changed {
		changedSet[p] = struct{}{}
		if strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			dirs[path.Dir(p)] = struct{}{}
		}
	}
	git := gitcmd.New(0)
	var out []Importer
	for dir := range dirs {
		importPath := mod
		if dir != "." {
			importPath = mod + "/" + dir
		}
		res, err := git.Run(ctx, repo, "grep", "-l", "-F", `"`+importPath+`"`, "--", "*.go")
		if err != nil {
			continue // exit 1 = no matches
		}
		for _, f := range strings.Split(strings.TrimSpace(res), "\n") {
			f = strings.TrimSpace(f)
			if f == "" || path.Dir(f) == dir || strings.HasSuffix(f, "_test.go") {
				continue
			}
			if _, ok := changedSet[f]; ok {
				continue
			}
			out = append(out, Importer{Path: f, Pkg: dir, Lines: referencingLines(filepath.Join(repo, f), path.Base(dir)+".")})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if len(out) > maxImporters {
		out = out[:maxImporters]
	}
	return out
}

// referencingLines returns up to maxImporterExcerpt "N: src" lines of file
// that contain needle (the package identifier followed by a dot).
func referencingLines(file, needle string) []string {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		if strings.Contains(sc.Text(), needle) {
			lines = append(lines, fmt.Sprintf("%d: %s", n, strings.TrimSpace(sc.Text())))
			if len(lines) == maxImporterExcerpt {
				break
			}
		}
	}
	return lines
}

// renderImporters produces the markdown block appended to the codebase
// context so the reviewer sees callers the diff did not touch.
func renderImporters(imps []Importer) string {
	if len(imps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Imported by (files outside this diff that use changed packages)\n\n")
	for _, im := range imps {
		fmt.Fprintf(&b, "- `%s` imports `%s`\n", im.Path, im.Pkg)
		for _, l := range im.Lines {
			fmt.Fprintf(&b, "  - `%s`\n", l)
		}
	}
	return b.String()
}

// goModulePath returns the module line of <repo>/go.mod, or "".
func goModulePath(repo string) string {
	data, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
