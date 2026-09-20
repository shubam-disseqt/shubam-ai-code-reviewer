// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package rules loads org-level review rules from a git-hosted rules
// repo, filters them by scope + path glob, and renders the injectable
// "## Custom Review Rules" block for the review prompt.
package rules

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/gitcmd"
)

// defaultBranch is used when neither the spec nor SACR_ORG_RULES_REF
// override the ref.
const defaultBranch = "main"

// refEnvVar is the environment variable that overrides the ref used when
// cloning / pulling the rules repo.
const refEnvVar = "SACR_ORG_RULES_REF"

// Loader clones or pulls an org-rules-repo into a cache directory and reads
// its YAML rule files.
type Loader struct {
	cacheDir string
	git      *gitcmd.Runner
}

// NewLoader returns a Loader that caches clones under cacheDir.
func NewLoader(cacheDir string) *Loader {
	return &Loader{
		cacheDir: cacheDir,
		git:      gitcmd.New(0),
	}
}

// LoadFromDir walks dir/rules/**/*.yaml (and *.yml), parses each file, and
// returns the aggregated rules. Used for local development and testing.
func (l *Loader) LoadFromDir(_ context.Context, dir string) ([]Rule, error) {
	rulesDir := filepath.Join(dir, "rules")
	info, err := os.Stat(rulesDir)
	if err != nil {
		return nil, fmt.Errorf("rules: cannot stat %s: %w", rulesDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("rules: %s is not a directory", rulesDir)
	}

	var files []string
	walkErr := filepath.WalkDir(rulesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			files = append(files, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("rules: walk %s: %w", rulesDir, walkErr)
	}

	// Deterministic order regardless of filesystem enumeration.
	sort.Strings(files)

	var all []Rule
	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("rules: read %s: %w", f, err)
		}
		parsed, err := Parse(content, f)
		if err != nil {
			return nil, err
		}
		all = append(all, parsed...)
	}
	return all, nil
}

// LoadFromRepo resolves spec into a local checkout and returns its rules.
//
// spec accepts:
//   - "file:///abs/path" or an absolute path — treated as a local dir
//     (LoadFromDir semantics).
//   - "owner/repo" or "owner/repo@ref" — shallow-cloned via git into
//     <cacheDir>/<owner>-<repo>. Subsequent calls fetch and reset.
//
// When spec omits a ref, the value of $SACR_ORG_RULES_REF is used, or
// "main" if that is unset.
func (l *Loader) LoadFromRepo(ctx context.Context, spec string) ([]Rule, error) {
	if spec == "" {
		return nil, errors.New("rules: empty repo spec")
	}

	// Local dir path.
	if strings.HasPrefix(spec, "file://") {
		return l.LoadFromDir(ctx, strings.TrimPrefix(spec, "file://"))
	}
	if filepath.IsAbs(spec) {
		return l.LoadFromDir(ctx, spec)
	}

	owner, repo, ref, err := parseRepoSpec(spec)
	if err != nil {
		return nil, err
	}
	if ref == "" {
		ref = envOr(refEnvVar, defaultBranch)
	}

	if l.cacheDir == "" {
		return nil, errors.New("rules: cache dir is required for git-hosted specs")
	}
	if err := os.MkdirAll(l.cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("rules: mkdir cache: %w", err)
	}
	dest := filepath.Join(l.cacheDir, owner+"-"+repo)

	if err := l.syncRepo(ctx, dest, owner, repo, ref); err != nil {
		return nil, err
	}
	return l.LoadFromDir(ctx, dest)
}

// repoURLBuilder maps (owner, repo) to a clone URL. Overridable in tests.
// note: package-level var so tests can swap in a local bare repo; upgrade to
// interface/DI if we ever grow a second git host or need per-Loader override.
var repoURLBuilder = func(owner, repo string) string {
	return fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
}

// syncRepo clones the repo on first use or fetches + resets an existing clone.
func (l *Loader) syncRepo(ctx context.Context, dest, owner, repo, ref string) error {
	url := repoURLBuilder(owner, repo)

	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("rules: stat %s: %w", dest, err)
		}
		// Fresh clone.
		if out, err := l.git.Run(ctx, "", "clone", "--depth=1", "--branch", ref, url, dest); err != nil {
			return fmt.Errorf("rules: git clone %s@%s failed: %w: %s", url, ref, err, strings.TrimSpace(out))
		}
		return nil
	}

	// Existing clone → fetch + hard reset.
	if out, err := l.git.Run(ctx, dest, "fetch", "--depth=1", "origin", ref); err != nil {
		return fmt.Errorf("rules: git fetch %s failed: %w: %s", ref, err, strings.TrimSpace(out))
	}
	if out, err := l.git.Run(ctx, dest, "reset", "--hard", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("rules: git reset failed: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// parseRepoSpec splits "owner/repo" or "owner/repo@ref" into components.
func parseRepoSpec(spec string) (owner, repo, ref string, err error) {
	s := spec
	if i := strings.Index(s, "@"); i >= 0 {
		ref = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("rules: invalid repo spec %q (want owner/repo[@ref])", spec)
	}
	return parts[0], parts[1], ref, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
