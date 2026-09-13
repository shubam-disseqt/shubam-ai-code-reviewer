// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package manifests

import (
	"sort"
	"strings"
	"testing"
)

// findByName returns the first Package with the given name, or a zero value.
func findByName(pkgs []Package, name string) (Package, bool) {
	for _, p := range pkgs {
		if p.Name == name {
			return p, true
		}
	}
	return Package{}, false
}

func names(pkgs []Package) []string {
	out := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

// ── Registry ──

func TestRegistryDispatch(t *testing.T) {
	r := NewRegistry()
	tests := []struct {
		path      string
		wantMatch bool
	}{
		{"package.json", true},
		{"src/package.json", true},
		{"package-lock.json", true},
		{"yarn.lock", true},
		{"pnpm-lock.yaml", true},
		{"go.mod", true},
		{"requirements.txt", true},
		{"requirements-dev.txt", true},
		{"pyproject.toml", true},
		{"Pipfile.lock", true},
		{"poetry.lock", true},
		{"uv.lock", true},
		{"Dockerfile", true},
		{"dockerfile", true},
		{"Containerfile", true},
		{"web.Dockerfile", true},
		{"composer.json", true},
		{"composer.lock", true},
		{"random.txt", false},
		{"README.md", false},
	}
	for _, tt := range tests {
		got := r.Match(tt.path)
		if got != tt.wantMatch {
			t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.wantMatch)
		}
	}
}

func TestRegistryParseUnknown(t *testing.T) {
	r := NewRegistry()
	pkgs, err := r.Parse("README.md", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("expected no packages, got %d", len(pkgs))
	}
}

func TestRegistryLockfileBeforeManifest(t *testing.T) {
	// Lockfile parsers must be positioned before their manifest peers.
	r := NewRegistry()
	pos := map[string]int{}
	for i, p := range r.Parsers() {
		switch p.(type) {
		case packageLockJSONParser:
			pos["package-lock"] = i
		case packageJSONParser:
			pos["package"] = i
		case uvLockParser:
			pos["uv"] = i
		case pyprojectTOMLParser:
			pos["pyproject"] = i
		case composerLockParser:
			pos["composer-lock"] = i
		case composerJSONParser:
			pos["composer"] = i
		}
	}
	if pos["package-lock"] >= pos["package"] {
		t.Errorf("package-lock.json must come before package.json")
	}
	if pos["uv"] >= pos["pyproject"] {
		t.Errorf("uv.lock must come before pyproject.toml")
	}
	if pos["composer-lock"] >= pos["composer"] {
		t.Errorf("composer.lock must come before composer.json")
	}
}

// ── package.json ──

func TestPackageJSONHappy(t *testing.T) {
	const src = `{
	  "name": "app",
	  "dependencies": { "express": "^4.18.0", "lodash": "4.17.21" },
	  "devDependencies": { "jest": "^29.0.0" },
	  "peerDependencies": { "react": ">=18" },
	  "optionalDependencies": { "fsevents": "^2.0.0" }
	}`
	pkgs, err := packageJSONParser{}.Parse(src, "package.json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 5 {
		t.Fatalf("want 5 packages, got %d", len(pkgs))
	}
	jest, _ := findByName(pkgs, "jest")
	if !jest.IsDev {
		t.Errorf("jest should be dev")
	}
	if jest.Kind != "npm" {
		t.Errorf("jest kind = %q", jest.Kind)
	}
	express, _ := findByName(pkgs, "express")
	if express.IsDev {
		t.Errorf("express must not be dev")
	}
}

func TestPackageJSONMalformed(t *testing.T) {
	_, err := packageJSONParser{}.Parse("{not json", "package.json")
	if err == nil {
		t.Error("expected error on malformed JSON")
	}
}

func TestPackageJSONEmpty(t *testing.T) {
	pkgs, err := packageJSONParser{}.Parse(`{}`, "package.json")
	if err != nil || len(pkgs) != 0 {
		t.Errorf("empty pkgs got err=%v len=%d", err, len(pkgs))
	}
}

func TestPackageJSONIgnoresNonStringVersion(t *testing.T) {
	// Malicious/malformed: version is an object.
	const src = `{"dependencies": {"foo": {"nope": 1}, "bar": "1.0"}}`
	pkgs, err := packageJSONParser{}.Parse(src, "package.json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "bar" {
		t.Errorf("expected only bar, got %v", names(pkgs))
	}
}

// ── package-lock.json ──

func TestPackageLockJSONv3(t *testing.T) {
	const src = `{
	  "lockfileVersion": 3,
	  "packages": {
	    "": {"name": "root", "version": "1.0.0"},
	    "node_modules/lodash": {"version": "4.17.21"},
	    "node_modules/@types/node": {"version": "20.5.0", "dev": true},
	    "node_modules/express/node_modules/debug": {"version": "2.6.9"}
	  }
	}`
	pkgs, err := packageLockJSONParser{}.Parse(src, "package-lock.json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got := names(pkgs)
	want := []string{"@types/node", "debug", "lodash"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("names = %v, want %v", got, want)
	}
	tn, _ := findByName(pkgs, "@types/node")
	if !tn.IsDev {
		t.Error("@types/node should be dev")
	}
}

func TestPackageLockJSONv1Legacy(t *testing.T) {
	const src = `{
	  "dependencies": {
	    "lodash": {"version": "4.17.21"},
	    "express": {"version": "4.18.0"}
	  }
	}`
	pkgs, err := packageLockJSONParser{}.Parse(src, "package-lock.json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("want 2, got %d", len(pkgs))
	}
}

func TestPackageLockJSONMalformed(t *testing.T) {
	_, err := packageLockJSONParser{}.Parse("garbage", "package-lock.json")
	if err == nil {
		t.Error("expected error")
	}
}

// ── yarn.lock ──

func TestYarnLock(t *testing.T) {
	const src = `# yarn lockfile v1

lodash@^4.17.20, lodash@^4.17.21:
  version "4.17.21"
  resolved "https://registry.yarnpkg.com/lodash/-/lodash-4.17.21.tgz"

"@babel/core@^7.0.0":
  version "7.22.0"
`
	pkgs, err := yarnLockParser{}.Parse(src, "yarn.lock")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("want 2, got %d: %v", len(pkgs), pkgs)
	}
	got := names(pkgs)
	want := []string{"@babel/core", "lodash"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("names = %v, want %v", got, want)
	}
	lo, _ := findByName(pkgs, "lodash")
	if lo.Version != "4.17.21" {
		t.Errorf("lodash version = %q", lo.Version)
	}
}

func TestYarnLockEmpty(t *testing.T) {
	pkgs, err := yarnLockParser{}.Parse("", "yarn.lock")
	if err != nil || len(pkgs) != 0 {
		t.Errorf("empty: err=%v len=%d", err, len(pkgs))
	}
}

// ── pnpm-lock.yaml ──

func TestPnpmLockModern(t *testing.T) {
	const src = `lockfileVersion: '6.0'

dependencies:
  lodash:
    specifier: ^4.17.21
    version: 4.17.21

packages:

  /lodash@4.17.21:
    resolution: {integrity: sha512-...}
    dev: false

  /@types/node@20.5.0:
    resolution: {integrity: sha512-...}
    dev: true

  /react@18.2.0(react-dom@18.2.0):
    resolution: {integrity: sha512-...}
`
	pkgs, err := pnpmLockParser{}.Parse(src, "pnpm-lock.yaml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got := names(pkgs)
	want := []string{"@types/node", "lodash", "react"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("names = %v, want %v", got, want)
	}
	r, _ := findByName(pkgs, "react")
	if r.Version != "18.2.0" {
		t.Errorf("react version = %q (peer suffix not stripped?)", r.Version)
	}
}

func TestPnpmLockEmpty(t *testing.T) {
	pkgs, err := pnpmLockParser{}.Parse("", "pnpm-lock.yaml")
	if err != nil || len(pkgs) != 0 {
		t.Errorf("empty: err=%v len=%d", err, len(pkgs))
	}
}

// ── go.mod ──

func TestGoMod(t *testing.T) {
	const src = `module example.com/x

go 1.22

require (
	github.com/foo/bar v1.2.3
	golang.org/x/sync v0.1.0 // indirect
)

require github.com/single/dep v0.0.1
`
	pkgs, err := goModParser{}.Parse(src, "go.mod")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 3 {
		t.Fatalf("want 3, got %d: %v", len(pkgs), pkgs)
	}
	sync, _ := findByName(pkgs, "golang.org/x/sync")
	if !sync.IsDev {
		t.Error("indirect should be marked dev")
	}
	foo, _ := findByName(pkgs, "github.com/foo/bar")
	if foo.IsDev {
		t.Error("direct should not be dev")
	}
	if foo.Kind != "go" || foo.Version != "v1.2.3" {
		t.Errorf("foo = %+v", foo)
	}
}

func TestGoModMalformed(t *testing.T) {
	_, err := goModParser{}.Parse("this is not go.mod!", "go.mod")
	if err == nil {
		t.Error("expected error on malformed go.mod")
	}
}

func TestGoModEmpty(t *testing.T) {
	// A go.mod with no require block is valid.
	pkgs, err := goModParser{}.Parse("module example.com/x\n\ngo 1.22\n", "go.mod")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("want 0, got %d", len(pkgs))
	}
}

// ── requirements.txt ──

func TestRequirementsTxt(t *testing.T) {
	const src = `# comment
requests==2.31.0
django>=4.2,<5.0
numpy ~= 1.26
flask
-e git+https://example.com/foo.git@main#egg=foo
./local
requests[security]==2.31.5   # trailing comment
`
	pkgs, err := requirementsTxtParser{}.Parse(src, "requirements.txt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got := names(pkgs)
	want := []string{"django", "flask", "numpy", "requests", "requests"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("names = %v, want %v", got, want)
	}
	for _, p := range pkgs {
		if p.IsDev {
			t.Errorf("requirements.txt (no dev/test in name) should not flag dev: %+v", p)
		}
	}
	// requests with extras stripped:
	haveExtras := false
	for _, p := range pkgs {
		if p.Name == "requests" && p.Version == "==2.31.5" {
			haveExtras = true
		}
	}
	if !haveExtras {
		t.Error("expected requests[security]==2.31.5 to be parsed with extras stripped")
	}
}

func TestRequirementsDevMarked(t *testing.T) {
	pkgs, _ := requirementsTxtParser{}.Parse("pytest==7.0\n", "requirements-dev.txt")
	if len(pkgs) != 1 || !pkgs[0].IsDev {
		t.Errorf("requirements-dev.txt should flag IsDev: %+v", pkgs)
	}
}

func TestRequirementsEmpty(t *testing.T) {
	pkgs, err := requirementsTxtParser{}.Parse("", "requirements.txt")
	if err != nil || len(pkgs) != 0 {
		t.Errorf("empty: err=%v len=%d", err, len(pkgs))
	}
}

// ── pyproject.toml ──

func TestPyprojectPEP621(t *testing.T) {
	const src = `[project]
name = "app"
dependencies = ["requests>=2.31.0", "django~=4.2"]

[project.optional-dependencies]
dev = ["pytest>=7.0", "ruff"]
docs = ["mkdocs"]
extras = ["numpy"]
`
	pkgs, err := pyprojectTOMLParser{}.Parse(src, "pyproject.toml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 6 {
		t.Fatalf("want 6, got %d: %v", len(pkgs), pkgs)
	}
	pytest, _ := findByName(pkgs, "pytest")
	if !pytest.IsDev {
		t.Error("pytest (in dev group) should be dev")
	}
	numpy, _ := findByName(pkgs, "numpy")
	if numpy.IsDev {
		t.Error("numpy (in `extras` group) must not be dev")
	}
	mkdocs, _ := findByName(pkgs, "mkdocs")
	if !mkdocs.IsDev {
		t.Error("mkdocs (in docs group) should be dev")
	}
}

func TestPyprojectPoetry(t *testing.T) {
	const src = `[tool.poetry]
name = "app"

[tool.poetry.dependencies]
python = "^3.11"
requests = "^2.31.0"
httpx = { version = "^0.27", extras = ["http2"] }

[tool.poetry.dev-dependencies]
black = "^23.0"

[tool.poetry.group.test.dependencies]
pytest = "^7.0"
`
	pkgs, err := pyprojectTOMLParser{}.Parse(src, "pyproject.toml")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, ok := findByName(pkgs, "python"); ok {
		t.Error("python constraint should be skipped")
	}
	req, _ := findByName(pkgs, "requests")
	if req.Version != "^2.31.0" {
		t.Errorf("requests version = %q", req.Version)
	}
	httpx, _ := findByName(pkgs, "httpx")
	if httpx.Version != "^0.27" {
		t.Errorf("httpx inline table version = %q", httpx.Version)
	}
	black, _ := findByName(pkgs, "black")
	if !black.IsDev {
		t.Error("black (dev-dependencies) should be dev")
	}
	pytest, _ := findByName(pkgs, "pytest")
	if !pytest.IsDev {
		t.Error("pytest (group.test) should be dev")
	}
}

func TestPyprojectMalformed(t *testing.T) {
	_, err := pyprojectTOMLParser{}.Parse("[project\nbroken", "pyproject.toml")
	if err == nil {
		t.Error("expected TOML error")
	}
}

func TestPyprojectEmpty(t *testing.T) {
	pkgs, err := pyprojectTOMLParser{}.Parse("", "pyproject.toml")
	if err != nil || len(pkgs) != 0 {
		t.Errorf("empty: err=%v len=%d", err, len(pkgs))
	}
}

// ── uv.lock / poetry.lock ──

func TestUvLock(t *testing.T) {
	const src = `version = 1

[[package]]
name = "requests"
version = "2.31.0"

[[package]]
name = "urllib3"
version = "2.0.4"
`
	pkgs, err := uvLockParser{}.Parse(src, "uv.lock")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("want 2, got %d", len(pkgs))
	}
	for _, p := range pkgs {
		if p.Kind != "pip" {
			t.Errorf("kind = %q, want pip", p.Kind)
		}
	}
}

func TestPoetryLock(t *testing.T) {
	const src = `[[package]]
name = "click"
version = "8.1.7"
`
	pkgs, err := poetryLockParser{}.Parse(src, "poetry.lock")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "click" {
		t.Errorf("got %v", pkgs)
	}
}

func TestUvLockMalformed(t *testing.T) {
	_, err := uvLockParser{}.Parse("[not\nvalid", "uv.lock")
	if err == nil {
		t.Error("expected TOML error")
	}
}

// ── Pipfile.lock ──

func TestPipfileLock(t *testing.T) {
	const src = `{
	  "default": {"requests": {"version": "==2.31.0"}},
	  "develop": {"pytest": {"version": "==7.4.0"}}
	}`
	pkgs, err := pipfileLockParser{}.Parse(src, "Pipfile.lock")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("want 2, got %d", len(pkgs))
	}
	pt, _ := findByName(pkgs, "pytest")
	if !pt.IsDev {
		t.Error("pytest (develop) should be dev")
	}
}

func TestPipfileLockMalformed(t *testing.T) {
	_, err := pipfileLockParser{}.Parse("not json", "Pipfile.lock")
	if err == nil {
		t.Error("expected JSON error")
	}
}

// ── Dockerfile ──

func TestDockerfile(t *testing.T) {
	const src = `# comment
FROM alpine:3.18 AS base
FROM python:3.11-slim
FROM scratch
FROM $BASE_IMAGE
`
	pkgs, err := dockerfileParser{}.Parse(src, "Dockerfile")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 4 {
		t.Fatalf("want 4, got %d: %v", len(pkgs), pkgs)
	}
	if pkgs[0].Name != "alpine" || pkgs[0].Version != "3.18" {
		t.Errorf("first = %+v", pkgs[0])
	}
	if pkgs[2].Name != "scratch" || pkgs[2].Version != "" {
		t.Errorf("scratch = %+v", pkgs[2])
	}
	if pkgs[3].Name != "$BASE_IMAGE" {
		t.Errorf("var = %+v", pkgs[3])
	}
}

func TestDockerfileMatchVariants(t *testing.T) {
	p := dockerfileParser{}
	for _, path := range []string{"Dockerfile", "dockerfile", "Containerfile", "app.Dockerfile"} {
		if !p.Match(path) {
			t.Errorf("Match(%q) = false", path)
		}
	}
	if p.Match("Dockerfile.bak") {
		t.Error("Dockerfile.bak should not match")
	}
}

func TestDockerfileEmpty(t *testing.T) {
	pkgs, err := dockerfileParser{}.Parse("", "Dockerfile")
	if err != nil || len(pkgs) != 0 {
		t.Errorf("empty: err=%v len=%d", err, len(pkgs))
	}
}

// ── composer ──

func TestComposerJSON(t *testing.T) {
	const src = `{
	  "require": {
	    "php": "^8.1",
	    "ext-json": "*",
	    "monolog/monolog": "^3.0"
	  },
	  "require-dev": {
	    "phpunit/phpunit": "^10.0"
	  }
	}`
	pkgs, err := composerJSONParser{}.Parse(src, "composer.json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("want 2 (php/ext- skipped), got %d: %v", len(pkgs), pkgs)
	}
	phpunit, _ := findByName(pkgs, "phpunit/phpunit")
	if !phpunit.IsDev {
		t.Error("phpunit (require-dev) should be dev")
	}
	mono, _ := findByName(pkgs, "monolog/monolog")
	if mono.IsDev {
		t.Error("monolog should not be dev")
	}
}

func TestComposerJSONMalformed(t *testing.T) {
	_, err := composerJSONParser{}.Parse("not json", "composer.json")
	if err == nil {
		t.Error("expected JSON error")
	}
}

func TestComposerLock(t *testing.T) {
	const src = `{
	  "packages": [
	    {"name": "monolog/monolog", "version": "3.5.0"}
	  ],
	  "packages-dev": [
	    {"name": "phpunit/phpunit", "version": "10.5.0"}
	  ]
	}`
	pkgs, err := composerLockParser{}.Parse(src, "composer.lock")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("want 2, got %d", len(pkgs))
	}
	dev, _ := findByName(pkgs, "phpunit/phpunit")
	if !dev.IsDev {
		t.Error("phpunit (packages-dev) should be dev")
	}
}

func TestComposerLockMalformed(t *testing.T) {
	_, err := composerLockParser{}.Parse("bogus", "composer.lock")
	if err == nil {
		t.Error("expected error")
	}
}

// ── PEP508 helper ──

func TestSplitPEP508(t *testing.T) {
	tests := []struct {
		in       string
		wantName string
		wantVer  string
	}{
		{"requests>=2.31.0", "requests", ">=2.31.0"},
		{`requests>=2.31.0; python_version>="3.8"`, "requests", ">=2.31.0"},
		{"requests[security]==2.31.0", "requests", "==2.31.0"},
		{"flask", "flask", ""},
		{"", "", ""},
		{"@#$%", "", ""},
	}
	for _, tt := range tests {
		n, v := splitPEP508(tt.in)
		if n != tt.wantName || v != tt.wantVer {
			t.Errorf("splitPEP508(%q) = (%q,%q), want (%q,%q)", tt.in, n, v, tt.wantName, tt.wantVer)
		}
	}
}

// ── End-to-end via Registry ──

func TestRegistryE2E(t *testing.T) {
	r := NewRegistry()
	pkgs, err := r.Parse("some/dir/package.json", `{"dependencies": {"foo": "1.0.0"}}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "foo" || pkgs[0].FilePath != "some/dir/package.json" {
		t.Errorf("got %v", pkgs)
	}
}

func TestRegistryLockDispatchesFirst(t *testing.T) {
	// If a path matches only the lockfile parser, it must dispatch to it.
	r := NewRegistry()
	pkgs, err := r.Parse("package-lock.json", `{"packages": {"node_modules/x": {"version": "1.0"}}}`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "x" || pkgs[0].Version != "1.0" {
		t.Errorf("got %v", pkgs)
	}
}
