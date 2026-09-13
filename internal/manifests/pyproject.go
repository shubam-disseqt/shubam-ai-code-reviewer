// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/manifests.py under Apache License 2.0.

package manifests

import (
	"strings"

	"github.com/BurntSushi/toml"
)

// pyprojectTOMLParser handles PEP 621 [project] and Poetry [tool.poetry].
type pyprojectTOMLParser struct{}

func (pyprojectTOMLParser) Match(path string) bool { return baseName(path) == "pyproject.toml" }

// pyproject is decoded loosely so we can handle both PEP 621 and Poetry.
// Poetry deps can be a string or an inline table, so we keep them as
// toml.Primitive and decode per-key.
type pyproject struct {
	Project struct {
		Dependencies         []string            `toml:"dependencies"`
		OptionalDependencies map[string][]string `toml:"optional-dependencies"`
	} `toml:"project"`
	Tool struct {
		Poetry struct {
			Dependencies map[string]toml.Primitive `toml:"dependencies"`
			Group        map[string]poetryGroup    `toml:"group"`
			DevDeps      map[string]toml.Primitive `toml:"dev-dependencies"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

type poetryGroup struct {
	Dependencies map[string]toml.Primitive `toml:"dependencies"`
}

func (pyprojectTOMLParser) Parse(content, filePath string) ([]Package, error) {
	var data pyproject
	md, err := toml.Decode(content, &data)
	if err != nil {
		return nil, err
	}
	var out []Package

	// PEP 621 main deps.
	for _, dep := range data.Project.Dependencies {
		if name, ver := splitPEP508(dep); name != "" {
			out = append(out, Package{Name: name, Kind: "pip", Version: ver, FilePath: filePath})
		}
	}
	// PEP 621 optional groups.
	for group, items := range data.Project.OptionalDependencies {
		isDev := isDevGroup(group)
		for _, dep := range items {
			if name, ver := splitPEP508(dep); name != "" {
				out = append(out, Package{
					Name:     name,
					Kind:     "pip",
					Version:  ver,
					FilePath: filePath,
					IsDev:    isDev,
				})
			}
		}
	}

	// Poetry main deps.
	for name, spec := range data.Tool.Poetry.Dependencies {
		if name == "python" {
			continue
		}
		ver := decodePoetryVersion(md, spec)
		out = append(out, Package{Name: name, Kind: "pip", Version: ver, FilePath: filePath})
	}
	// Poetry legacy dev-dependencies (pre-groups format).
	for name, spec := range data.Tool.Poetry.DevDeps {
		ver := decodePoetryVersion(md, spec)
		out = append(out, Package{
			Name:     name,
			Kind:     "pip",
			Version:  ver,
			FilePath: filePath,
			IsDev:    true,
		})
	}
	// Poetry groups.
	for groupName, group := range data.Tool.Poetry.Group {
		isDev := isDevGroup(groupName)
		for name, spec := range group.Dependencies {
			ver := decodePoetryVersion(md, spec)
			out = append(out, Package{
				Name:     name,
				Kind:     "pip",
				Version:  ver,
				FilePath: filePath,
				IsDev:    isDev,
			})
		}
	}
	return out, nil
}

// decodePoetryVersion handles the two Poetry dep shapes:
//
//	name = "^1.2.3"          → primitive is a string
//	name = { version = "^1.2.3", extras = [...] }  → inline table
func decodePoetryVersion(md toml.MetaData, prim toml.Primitive) string {
	// Try string first.
	var s string
	if err := md.PrimitiveDecode(prim, &s); err == nil {
		return s
	}
	// Try inline table.
	var t struct {
		Version string `toml:"version"`
	}
	if err := md.PrimitiveDecode(prim, &t); err == nil {
		return t.Version
	}
	return ""
}

func isDevGroup(name string) bool {
	switch strings.ToLower(name) {
	case "dev", "test", "testing", "lint", "docs":
		return true
	}
	return false
}
