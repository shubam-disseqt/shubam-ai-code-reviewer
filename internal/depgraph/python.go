// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package depgraph

import (
	"path"
	"regexp"
	"strings"
)

// pyImportRe matches Python's two import shapes:
//   - `import X` or `import X.Y.Z` (group 1 captures the module)
//   - `from [.]*X import Y` (group 2 = dots, group 3 = module; module may be empty for `from . import x`)
//
// Multiple targets on the same line (`import a, b`) collapse to the first;
// review diagrams rarely need every alias. `as` clauses are ignored — we
// only care about the source module.
var pyImportRe = regexp.MustCompile(
	`(?m)^\s*(?:import\s+([A-Za-z_][\w.]*)` +
		`|from\s+(\.*)([A-Za-z_][\w.]*)?\s+import\s+)`,
)

// extractPython scans a .py file for imports and records project-local
// edges. Relative imports (`from .foo import X`) resolve against the
// source file's package directory. Absolute imports are considered
// in-repo only when they start with ModulePrefix (typically the top
// package name, e.g. "myapp." or "myapp"). Everything else is external.
func extractPython(f File, opts Options, seen map[edge]struct{}) {
	srcPkg := shortPackage(path.Dir(f.Path), "")
	if srcPkg == "" {
		return
	}
	for _, m := range pyImportRe.FindAllSubmatch(f.Content, -1) {
		var spec string
		relDots := 0
		switch {
		case len(m[1]) > 0:
			// `import X.Y`
			spec = string(m[1])
		default:
			// `from .X import Y` or `from X import Y`
			relDots = len(m[2])
			spec = string(m[3])
		}
		dst := resolvePySpec(spec, relDots, path.Dir(f.Path), opts.ModulePrefix)
		if dst == "" {
			if !opts.IncludeExternal || spec == "" {
				continue
			}
			dst = spec
		}
		if dst == "" || dst == srcPkg {
			continue
		}
		seen[edge{Src: srcPkg, Dst: dst}] = struct{}{}
	}
}

// resolvePySpec converts a Python module spec + relative-dot count into a
// repo-relative path label, or "" for external modules.
//
// Rules:
//   - relDots > 0: relative import. Walk up (relDots-1) directories from
//     srcDir, then append spec. `from . import x` (dots=1, spec="") →
//     srcDir itself, which we skip via the self-edge guard.
//   - relDots == 0 and modulePrefix set: must start with modulePrefix to
//     count as in-repo. Prefix is stripped, dots become slashes.
//   - Otherwise: external (return "").
func resolvePySpec(spec string, relDots int, srcDir, modulePrefix string) string {
	if relDots > 0 {
		base := srcDir
		for i := 1; i < relDots; i++ {
			base = path.Dir(base)
		}
		if spec == "" {
			return shortPackage(base, "")
		}
		return shortPackage(path.Join(base, strings.ReplaceAll(spec, ".", "/")), "")
	}
	if modulePrefix == "" || spec == "" {
		return ""
	}
	trimmed := strings.TrimSuffix(modulePrefix, ".")
	if spec == trimmed || strings.HasPrefix(spec, trimmed+".") {
		rest := strings.TrimPrefix(strings.TrimPrefix(spec, trimmed), ".")
		if rest == "" {
			return trimmed
		}
		return trimmed + "/" + strings.ReplaceAll(rest, ".", "/")
	}
	return ""
}
