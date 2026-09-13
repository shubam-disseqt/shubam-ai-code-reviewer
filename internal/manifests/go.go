// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/manifests.py under Apache License 2.0.

package manifests

import "golang.org/x/mod/modfile"

// goModParser parses `go.mod` via golang.org/x/mod/modfile.
//
// Indirect dependencies are flagged as IsDev to mirror Mira's behavior
// (Mira reuses the is_dev field for `// indirect` markers even though
// "indirect" and "dev" aren't the same thing semantically — this preserves
// the same downstream contract).
type goModParser struct{}

func (goModParser) Match(path string) bool { return baseName(path) == "go.mod" }

func (goModParser) Parse(content, filePath string) ([]Package, error) {
	f, err := modfile.Parse(filePath, []byte(content), nil)
	if err != nil {
		return nil, err
	}
	var out []Package
	for _, r := range f.Require {
		if r == nil {
			continue
		}
		out = append(out, Package{
			Name:     r.Mod.Path,
			Kind:     "go",
			Version:  r.Mod.Version,
			FilePath: filePath,
			IsDev:    r.Indirect,
		})
	}
	return out, nil
}
