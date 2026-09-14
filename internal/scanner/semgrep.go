// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shubam-disseqt/z-code-reviewer/internal/scanner/rules"
)

// semgrepScanner shells out to semgrep and parses the top-level results
// list from its --json output.
type semgrepScanner struct {
	stderr io.Writer
}

func (s *semgrepScanner) Name() string { return "semgrep" }

// semgrepResult mirrors one entry in the semgrep JSON `results` array.
// We deliberately keep only fields we surface — semgrep exposes ~30 keys
// per result but most are for its own CI product, not for review comments.
type semgrepResult struct {
	CheckID string          `json:"check_id"`
	Path    string          `json:"path"`
	Start   semgrepLocation `json:"start"`
	End     semgrepLocation `json:"end"`
	Extra   semgrepExtra    `json:"extra"`
}

type semgrepLocation struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

type semgrepExtra struct {
	Message  string `json:"message"`
	Severity string `json:"severity"`
	Lines    string `json:"lines"`
}

type semgrepOutput struct {
	Results []semgrepResult `json:"results"`
}

// preset identifies one bundled ruleset per language family.
type preset struct {
	name string // filename stem, used for the temp file
	yaml []byte
}

var (
	presetJS     = preset{name: "javascript", yaml: rules.JavaScript}
	presetPython = preset{name: "python", yaml: rules.Python}
	presetRuby   = preset{name: "ruby", yaml: rules.Ruby}
)

func (s *semgrepScanner) Run(ctx context.Context, repoRoot string, changedPaths []string) ([]ScannerFinding, error) {
	if _, err := exec.LookPath("semgrep"); err != nil {
		return nil, fmt.Errorf("skipping semgrep (not installed)")
	}

	configs, cleanup, err := resolveSemgrepConfigs(changedPaths)
	if err != nil {
		return nil, fmt.Errorf("resolve semgrep config: %w", err)
	}
	defer cleanup()
	if len(configs) == 0 {
		// Nothing to scan — no supported language in the diff, and no
		// user override. Skip cleanly rather than shelling out with
		// --config auto and pulling rules over the network.
		return nil, nil
	}

	// Prefer to scan only the changed paths to keep semgrep bounded on
	// large repos. If the caller passed no paths, fall back to the whole
	// repo so a broken caller doesn't accidentally silence the scanner.
	targets := changedPaths
	if len(targets) == 0 {
		targets = []string{repoRoot}
	}

	args := []string{"--json", "--quiet", "--disable-version-check", "--metrics=off"}
	for _, cfg := range configs {
		args = append(args, "--config", cfg)
	}
	args = append(args, targets...)

	// Fresh stdout buffer per attempt so a retry doesn't concatenate two
	// runs' JSON.
	var stdout bytes.Buffer
	err = execAttempt(ctx, func() error {
		stdout.Reset()
		//nolint:gosec // targets are repo-relative paths from the deterministic selector
		cmd := exec.CommandContext(ctx, "semgrep", args...)
		cmd.Dir = repoRoot
		cmd.Stderr = s.stderr
		cmd.Stdout = &stdout
		return cmd.Run()
	})
	if err != nil {
		// semgrep exits non-zero when it finds issues (with --error)
		// OR when config download fails. We didn't set --error so any
		// non-zero exit is a real failure; report + skip.
		return nil, fmt.Errorf("semgrep exec: %w", err)
	}

	if strings.TrimSpace(stdout.String()) == "" {
		return nil, nil
	}
	var parsed semgrepOutput
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("parse output: %w", err)
	}
	return convertSemgrep(parsed.Results), nil
}

// resolveSemgrepConfigs decides which --config value(s) to pass to
// semgrep. Priority:
//  1. ZREVIEW_SEMGREP_CONFIG (user override, passed through verbatim).
//  2. Bundled presets matching the languages in changedPaths, unless
//     ZREVIEW_DISABLE_SEMGREP_PRESETS=1.
//
// Returns the list of config args and a cleanup fn that removes any
// temp files created for embedded rules.
func resolveSemgrepConfigs(changedPaths []string) ([]string, func(), error) {
	noop := func() {}
	if override := strings.TrimSpace(os.Getenv("ZREVIEW_SEMGREP_CONFIG")); override != "" {
		return []string{override}, noop, nil
	}
	if os.Getenv("ZREVIEW_DISABLE_SEMGREP_PRESETS") == "1" {
		return nil, noop, nil
	}
	presets := selectPresets(changedPaths)
	if len(presets) == 0 {
		return nil, noop, nil
	}

	tmpDir, err := os.MkdirTemp("", "zreview-semgrep-*")
	if err != nil {
		return nil, noop, fmt.Errorf("mkdir temp: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	paths := make([]string, 0, len(presets))
	for _, p := range presets {
		path := filepath.Join(tmpDir, p.name+".yml")
		if err := os.WriteFile(path, p.yaml, 0o600); err != nil {
			cleanup()
			return nil, noop, fmt.Errorf("write %s preset: %w", p.name, err)
		}
		paths = append(paths, path)
	}
	return paths, cleanup, nil
}

// selectPresets returns the bundled rulesets whose language actually
// appears in the changed paths. Go files intentionally do not select a
// preset — govulncheck covers Go and semgrep on it would be duplicate
// noise.
func selectPresets(changedPaths []string) []preset {
	var js, py, rb bool
	for _, p := range changedPaths {
		switch {
		case matchesLang(p, ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"):
			js = true
		case matchesLang(p, ".py", ".pyi"):
			py = true
		case matchesLang(p, ".rb", ".rake"):
			rb = true
		case strings.EqualFold(filepath.Base(p), "Gemfile"):
			rb = true
		}
	}
	out := make([]preset, 0, 3)
	if js {
		out = append(out, presetJS)
	}
	if py {
		out = append(out, presetPython)
	}
	if rb {
		out = append(out, presetRuby)
	}
	return out
}

func matchesLang(path string, exts ...string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// convertSemgrep maps parsed results into normalised findings. Extracted
// so unit tests can drive the mapping without invoking semgrep itself.
func convertSemgrep(results []semgrepResult) []ScannerFinding {
	out := make([]ScannerFinding, 0, len(results))
	for _, r := range results {
		out = append(out, ScannerFinding{
			Tool:        "semgrep",
			RuleID:      r.CheckID,
			Path:        r.Path,
			Line:        r.Start.Line,
			Kind:        KindSAST,
			Severity:    mapSemgrepSeverity(r.Extra.Severity),
			Description: firstNonEmpty(r.Extra.Message, r.CheckID),
			Evidence:    strings.TrimSpace(r.Extra.Lines),
		})
	}
	return out
}

// mapSemgrepSeverity maps semgrep's three-level severity onto our
// five-level scale. Semgrep does not distinguish critical from high — we
// leave critical for CVE / secret findings where the notion is meaningful.
func mapSemgrepSeverity(raw string) Severity {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "ERROR":
		return SeverityHigh
	case "WARNING":
		return SeverityMedium
	case "INFO":
		return SeverityInfo
	default:
		return SeverityMedium
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
