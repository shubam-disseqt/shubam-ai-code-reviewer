// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package depgraph builds a deterministic Go import graph from the source
// text of a PR's changed files. Every edge in the output corresponds to a
// literal `import` line in the parsed Go source — no LLM inference, no
// architecture-from-filenames guessing.
//
// The output is a Mermaid flowchart body ready to drop inside a
// ```mermaid``` fence. Empty output when there are no cross-package edges
// worth showing.
package depgraph

import (
	"fmt"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strings"
)

// Options tune the graph. ModulePrefix is the module path of the repo (e.g.
// "github.com/shubam-disseqt/z-code-reviewer"); imports that share this
// prefix are considered "in-repo" and get their package path rendered
// stripped of the prefix. External imports (stdlib, third-party) are
// dropped unless IncludeExternal is true.
type Options struct {
	ModulePrefix    string
	IncludeExternal bool
	// MaxNodes caps the rendered graph size so a monorepo PR doesn't
	// produce an unreadable diagram. Nodes past the cap are grouped
	// into a single "…and N more" placeholder.
	MaxNodes int
}

// DefaultOptions returns a sensible default for zreview review.
func DefaultOptions(modulePrefix string) Options {
	return Options{
		ModulePrefix:    modulePrefix,
		IncludeExternal: false,
		MaxNodes:        24,
	}
}

// File represents one changed file the caller wants included in the graph.
// Path is repo-relative (e.g. "cmd/api/main.go"); Content is the raw Go
// source at the reviewed head.
type File struct {
	Path    string
	Content []byte
}

// Render parses each Go file's imports and returns a Mermaid flowchart
// body. Non-Go files are ignored silently. Empty return means either no Go
// files, or no cross-package imports worth drawing — the caller should
// omit the section in that case.
func Render(files []File, opts Options) string {
	edges := extractEdges(files, opts)
	if len(edges) == 0 {
		return ""
	}
	return renderMermaid(edges, opts.MaxNodes)
}

// edge is one directed edge from src package → dst package. Both are
// short labels — repo-prefix stripped, "cmd/api" not the module URL.
type edge struct {
	Src, Dst string
}

// extractEdges parses every changed file and returns unique directed
// import edges. Duplicate imports across multiple files in the same
// package collapse into one edge.
func extractEdges(files []File, opts Options) []edge {
	seen := make(map[edge]struct{})
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		ast, err := parser.ParseFile(fset, f.Path, f.Content, parser.ImportsOnly)
		if err != nil {
			// Malformed source: skip this file, not the whole diff.
			continue
		}
		srcPkg := shortPackage(path.Dir(f.Path), opts.ModulePrefix)
		for _, imp := range ast.Imports {
			raw := strings.Trim(imp.Path.Value, `"`)
			if !opts.IncludeExternal && !isInRepo(raw, opts.ModulePrefix) {
				continue
			}
			dstPkg := shortPackage(strings.TrimPrefix(raw, opts.ModulePrefix+"/"), opts.ModulePrefix)
			if dstPkg == "" || srcPkg == "" || srcPkg == dstPkg {
				continue
			}
			seen[edge{Src: srcPkg, Dst: dstPkg}] = struct{}{}
		}
	}
	out := make([]edge, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Src != out[j].Src {
			return out[i].Src < out[j].Src
		}
		return out[i].Dst < out[j].Dst
	})
	return out
}

// isInRepo tells whether an import path belongs to the current repo's
// module. Empty module prefix means "everything counts as external".
func isInRepo(importPath, modulePrefix string) bool {
	if modulePrefix == "" {
		return false
	}
	return importPath == modulePrefix ||
		strings.HasPrefix(importPath, modulePrefix+"/")
}

// shortPackage produces the label shown in the diagram. For "cmd/api/main.go"
// (a file path) → "cmd/api". For "internal/auth" (already a directory) →
// "internal/auth". External imports like "net/http" get returned as-is
// when IncludeExternal is true.
func shortPackage(dirOrPath, modulePrefix string) string {
	dirOrPath = strings.TrimPrefix(dirOrPath, modulePrefix+"/")
	dirOrPath = strings.TrimPrefix(dirOrPath, "./")
	dirOrPath = strings.TrimSuffix(dirOrPath, "/")
	if dirOrPath == "" || dirOrPath == "." {
		return ""
	}
	return dirOrPath
}

// renderMermaid produces the flowchart body. Nodes are auto-named A1, A2,
// … so package paths with special characters (`.`, `/`) never appear as
// bare Mermaid identifiers. Labels use `["cmd/api"]` bracket syntax
// which Mermaid renders as-is, quotes included.
func renderMermaid(edges []edge, maxNodes int) string {
	nodes := collectNodes(edges)
	if maxNodes > 0 && len(nodes) > maxNodes {
		nodes = nodes[:maxNodes]
	}
	// alias every node to a short id so package paths with "/" don't
	// confuse the Mermaid parser.
	alias := make(map[string]string, len(nodes))
	for i, n := range nodes {
		alias[n] = fmt.Sprintf("N%d", i)
	}
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", alias[n], n)
	}
	for _, e := range edges {
		srcID, srcOK := alias[e.Src]
		dstID, dstOK := alias[e.Dst]
		if !srcOK || !dstOK {
			continue // one of the nodes got trimmed by MaxNodes
		}
		fmt.Fprintf(&b, "  %s --> %s\n", srcID, dstID)
	}
	if maxNodes > 0 && len(collectNodes(edges)) > maxNodes {
		fmt.Fprintf(&b, "  more[\"… and %d more\"]\n", len(collectNodes(edges))-maxNodes)
	}
	return strings.TrimRight(b.String(), "\n")
}

// collectNodes returns every unique package that appears in the edge list,
// sorted alphabetically so the output is deterministic.
func collectNodes(edges []edge) []string {
	seen := make(map[string]struct{})
	for _, e := range edges {
		seen[e.Src] = struct{}{}
		seen[e.Dst] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
