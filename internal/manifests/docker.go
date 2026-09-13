// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/manifests.py under Apache License 2.0.

package manifests

import (
	"regexp"
	"strings"
)

type dockerfileParser struct{}

// Match accepts Dockerfile (both cases), Containerfile, and *.Dockerfile.
func (dockerfileParser) Match(path string) bool {
	base := baseName(path)
	if base == "Dockerfile" || base == "dockerfile" || base == "Containerfile" {
		return true
	}
	return strings.HasSuffix(base, ".Dockerfile")
}

// dockerFROM matches `FROM image[:tag] [AS name]`. Case-insensitive.
var dockerFROM = regexp.MustCompile(`(?i)^\s*FROM\s+(\S+)(?:\s+AS\s+\S+)?\s*$`)

func (dockerfileParser) Parse(content, filePath string) ([]Package, error) {
	var out []Package
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		m := dockerFROM.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		image := m[1]
		// Skip variable-only images like $BASE — no way to resolve without eval.
		if strings.HasPrefix(image, "$") {
			out = append(out, Package{Name: image, Kind: "docker", FilePath: filePath})
			continue
		}
		if i := strings.LastIndex(image, ":"); i >= 0 {
			out = append(out, Package{
				Name:     image[:i],
				Kind:     "docker",
				Version:  image[i+1:],
				FilePath: filePath,
			})
		} else {
			out = append(out, Package{Name: image, Kind: "docker", FilePath: filePath})
		}
	}
	return out, nil
}
