// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/manifests.py under Apache License 2.0.

package manifests

import (
	"encoding/json"
	"strings"
)

// ── package.json ──────────────────────────────────────────────────────────────

type packageJSONParser struct{}

func (packageJSONParser) Match(path string) bool { return baseName(path) == "package.json" }

// packageJSON captures the four dependency blocks we care about.
type packageJSON struct {
	Dependencies         map[string]any `json:"dependencies"`
	DevDependencies      map[string]any `json:"devDependencies"`
	PeerDependencies     map[string]any `json:"peerDependencies"`
	OptionalDependencies map[string]any `json:"optionalDependencies"`
}

func (packageJSONParser) Parse(content, filePath string) ([]Package, error) {
	var data packageJSON
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, err
	}
	var out []Package
	blocks := []struct {
		m     map[string]any
		isDev bool
	}{
		{data.Dependencies, false},
		{data.DevDependencies, true},
		{data.PeerDependencies, false},
		{data.OptionalDependencies, false},
	}
	for _, b := range blocks {
		for name, v := range b.m {
			ver, ok := v.(string)
			if !ok {
				continue
			}
			out = append(out, Package{
				Name:     name,
				Kind:     "npm",
				Version:  strings.TrimSpace(ver),
				FilePath: filePath,
				IsDev:    b.isDev,
			})
		}
	}
	return out, nil
}

// ── package-lock.json ─────────────────────────────────────────────────────────

type packageLockJSONParser struct{}

func (packageLockJSONParser) Match(path string) bool {
	return baseName(path) == "package-lock.json"
}

type packageLockEntry struct {
	Version string `json:"version"`
	Dev     bool   `json:"dev"`
}

type packageLockJSON struct {
	// npm v2/v3 flat map keyed by "node_modules/x[/node_modules/y]".
	Packages map[string]packageLockEntry `json:"packages"`
	// npm v1 nested tree.
	Dependencies map[string]packageLockEntry `json:"dependencies"`
}

func (packageLockJSONParser) Parse(content, filePath string) ([]Package, error) {
	var data packageLockJSON
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, err
	}
	var out []Package
	if len(data.Packages) > 0 {
		for path, info := range data.Packages {
			if path == "" || info.Version == "" {
				continue // root project entry has key ""
			}
			name := extractNpmName(path)
			if name == "" {
				continue
			}
			out = append(out, Package{
				Name:     name,
				Kind:     "npm",
				Version:  info.Version,
				FilePath: filePath,
				IsDev:    info.Dev,
			})
		}
		return out, nil
	}
	// Legacy v1.
	for name, info := range data.Dependencies {
		if info.Version == "" {
			continue
		}
		out = append(out, Package{
			Name:     name,
			Kind:     "npm",
			Version:  info.Version,
			FilePath: filePath,
		})
	}
	return out, nil
}

// extractNpmName pulls the package name from a lockfile path like
// "node_modules/x" or "node_modules/@scope/name/node_modules/inner".
func extractNpmName(path string) string {
	// Take the segment after the LAST "node_modules/".
	idx := strings.LastIndex(path, "node_modules/")
	tail := path
	if idx >= 0 {
		tail = path[idx+len("node_modules/"):]
	}
	if tail == "" {
		return ""
	}
	if strings.HasPrefix(tail, "@") {
		// Scoped: "@scope/name[/...]"
		segs := strings.SplitN(tail, "/", 3)
		if len(segs) < 2 {
			return ""
		}
		return segs[0] + "/" + segs[1]
	}
	return strings.SplitN(tail, "/", 2)[0]
}

// ── yarn.lock ─────────────────────────────────────────────────────────────────
//
// yarn.lock is a hand-rolled INI-ish format. Each top-level block starts with
// one or more quoted specifiers ("lodash@^4.17.20":) followed by indented
// key/value pairs including `version "4.17.21"`. We only need name + version.

type yarnLockParser struct{}

func (yarnLockParser) Match(path string) bool { return baseName(path) == "yarn.lock" }

func (yarnLockParser) Parse(content, filePath string) ([]Package, error) {
	var out []Package
	var currentName string
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		// Block header: not indented and ends with ':'.
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && strings.HasSuffix(line, ":") {
			// Header can be one or more comma-separated specifiers.
			// Take the first, strip quotes, parse "name@range".
			header := strings.TrimSuffix(line, ":")
			first := strings.SplitN(header, ",", 2)[0]
			first = strings.TrimSpace(first)
			first = strings.Trim(first, `"`)
			currentName = yarnSpecName(first)
			continue
		}
		if currentName == "" {
			continue
		}
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "version ") || strings.HasPrefix(trim, "version\t") {
			// version "4.17.21"
			v := strings.TrimSpace(strings.TrimPrefix(trim, "version"))
			v = strings.Trim(v, `"`)
			if v != "" {
				out = append(out, Package{
					Name:     currentName,
					Kind:     "npm",
					Version:  v,
					FilePath: filePath,
				})
			}
			currentName = ""
		}
	}
	return out, nil
}

// yarnSpecName parses "name@range" or "@scope/name@range" → name.
func yarnSpecName(spec string) string {
	if spec == "" {
		return ""
	}
	if strings.HasPrefix(spec, "@") {
		// @scope/name@range — the second '@' is the separator.
		i := strings.Index(spec[1:], "@")
		if i < 0 {
			return spec
		}
		return spec[:i+1]
	}
	i := strings.Index(spec, "@")
	if i < 0 {
		return spec
	}
	return spec[:i]
}

// ── pnpm-lock.yaml ────────────────────────────────────────────────────────────
//
// pnpm-lock.yaml is YAML. Hand-parser (avoiding a YAML dep) that reads
// top-level `packages:` map: each key is "/name/version" or "/@scope/name/version".
// We ignore other sections. Only two-space indentation is used by pnpm.

type pnpmLockParser struct{}

func (pnpmLockParser) Match(path string) bool { return baseName(path) == "pnpm-lock.yaml" }

func (pnpmLockParser) Parse(content, filePath string) ([]Package, error) {
	var out []Package
	inPackages := false
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		// Top-level key?
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inPackages = strings.HasPrefix(line, "packages:")
			continue
		}
		if !inPackages {
			continue
		}
		// pnpm entries look like:  "  /lodash@4.17.21:"  or  "  /@types/node@20.0.0:"
		// Older format used "/name/version:".
		trim := strings.TrimSpace(line)
		if !strings.HasSuffix(trim, ":") || !strings.HasPrefix(trim, "/") {
			continue
		}
		key := strings.TrimSuffix(trim, ":")
		key = strings.Trim(key, `'"`)
		if !strings.HasPrefix(key, "/") {
			continue
		}
		spec := key[1:] // drop leading '/'
		if name, version := splitPnpmSpec(spec); name != "" && version != "" {
			out = append(out, Package{
				Name:     name,
				Kind:     "npm",
				Version:  version,
				FilePath: filePath,
			})
		}
	}
	return out, nil
}

// splitPnpmSpec handles both "name@version" and legacy "name/version" forms,
// with scope awareness. Peer suffixes like "(react@18)" are trimmed off.
func splitPnpmSpec(spec string) (name, version string) {
	if spec == "" {
		return "", ""
	}
	// Trim trailing peer-dep suffix "(...)".
	if i := strings.Index(spec, "("); i >= 0 {
		spec = spec[:i]
	}
	// Modern: "@scope/name@version" or "name@version".
	if strings.HasPrefix(spec, "@") {
		i := strings.Index(spec[1:], "@")
		if i > 0 {
			return spec[:i+1], spec[i+2:]
		}
	} else if i := strings.Index(spec, "@"); i > 0 {
		return spec[:i], spec[i+1:]
	}
	// Legacy "/name/version" (already stripped leading '/').
	if strings.HasPrefix(spec, "@") {
		// @scope/name/version
		parts := strings.SplitN(spec, "/", 3)
		if len(parts) == 3 {
			return parts[0] + "/" + parts[1], parts[2]
		}
		return "", ""
	}
	parts := strings.SplitN(spec, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}
