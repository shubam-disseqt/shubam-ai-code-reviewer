// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

// scannerImpl is the internal contract every adapter implements. Kept
// unexported so callers wire adapters via Run(), not by constructing their
// own runner list.
type scannerImpl interface {
	// Name is the binary/tool name for logging. Must match ScannerFinding.Tool.
	Name() string
	// Run executes the tool against repoRoot and returns findings. Any
	// non-nil error returned (except ctx.Err) is treated as best-effort
	// and swallowed with a log line — a broken tool must never fail the
	// pipeline. changedPaths is the deterministic list of paths that passed
	// the selector; adapters may narrow the scan to those paths where the
	// tool supports it (semgrep), or ignore them (govulncheck, gitleaks
	// which scan the whole tree).
	Run(ctx context.Context, repoRoot string, changedPaths []string) ([]ScannerFinding, error)
}

// LogFunc receives one-line info logs from the runner. Defaults to
// log.Printf with the "[sacr] scanner:" prefix.
type LogFunc func(format string, args ...any)

// Options configure the top-level Run call. Zero-value is fine for
// production use.
type Options struct {
	// Log receives info lines (one per skipped/successful adapter). Defaults
	// to log.Printf when nil.
	Log LogFunc
	// Disabled is the set of adapter names to skip entirely. Filled from
	// SACR_DISABLE_SCANNERS when Run is called via the CLI helper.
	Disabled map[string]struct{}
	// Scanners overrides the default adapter list. Nil means "the three
	// production adapters".
	Scanners []scannerImpl
	// Stderr receives raw tool stderr for debugging. Defaults to io.Discard.
	Stderr io.Writer
}

// Run executes every enabled scanner concurrently and returns the combined
// finding list. Only ctx cancellation surfaces as an error — every other
// failure mode (binary missing, non-zero exit, JSON parse error) is logged
// and treated as an empty result from that adapter.
func Run(ctx context.Context, repoRoot string, changedPaths []string, opts Options) ([]ScannerFinding, error) {
	if opts.Log == nil {
		opts.Log = func(format string, args ...any) {
			log.Printf("[sacr] scanner: "+format, args...)
		}
	}
	// Serialise log calls: adapters run in parallel goroutines and the
	// caller's LogFunc may not be safe for concurrent use.
	logMu := &sync.Mutex{}
	rawLog := opts.Log
	safeLog := func(format string, args ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		rawLog(format, args...)
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	scanners := opts.Scanners
	if scanners == nil {
		scanners = defaultScanners(opts.Stderr)
	}

	// Filter disabled scanners early so the log line is honest about what
	// actually ran.
	enabled := make([]scannerImpl, 0, len(scanners))
	for _, s := range scanners {
		if _, off := opts.Disabled[s.Name()]; off {
			safeLog("skipping %s (disabled via SACR_DISABLE_SCANNERS)", s.Name())
			continue
		}
		enabled = append(enabled, s)
	}

	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	var out []ScannerFinding
	for _, s := range enabled {
		s := s
		g.Go(func() error {
			findings, err := s.Run(gctx, repoRoot, changedPaths)
			if err != nil {
				// Surface ONLY ctx cancellation. Anything else is
				// best-effort and gets logged.
				if gctx.Err() != nil {
					return gctx.Err()
				}
				safeLog("%s: %v (continuing)", s.Name(), err)
				return nil
			}
			mu.Lock()
			out = append(out, findings...)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	// Deterministic order for downstream comparison. Sort by tool, path,
	// line, rule.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Tool != b.Tool {
			return a.Tool < b.Tool
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.RuleID < b.RuleID
	})
	return out, nil
}

// ParseDisabled reads a comma/space-separated list of scanner names and
// returns a set suitable for Options.Disabled. Case-insensitive.
func ParseDisabled(spec string) map[string]struct{} {
	if spec == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, tok := range strings.FieldsFunc(spec, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	}) {
		name := strings.ToLower(strings.TrimSpace(tok))
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// defaultScanners returns the three production adapters in a stable order.
func defaultScanners(stderr io.Writer) []scannerImpl {
	return []scannerImpl{
		&gitleaksScanner{stderr: stderr},
		&semgrepScanner{stderr: stderr},
		&govulncheckScanner{stderr: stderr},
	}
}

// tallyByTool returns a "gitleaks:X, semgrep:Y, govulncheck:Z" style
// summary line for logging. Guarantees each production tool name shows up
// even when its count is zero so operators can tell "0 findings" from
// "adapter didn't run".
func TallyByTool(findings []ScannerFinding) string {
	counts := map[string]int{
		"gitleaks":    0,
		"semgrep":     0,
		"govulncheck": 0,
	}
	for _, f := range findings {
		counts[f.Tool]++
	}
	// Stable order matches the tallyByTool comment above.
	order := []string{"gitleaks", "semgrep", "govulncheck"}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		parts = append(parts, fmt.Sprintf("%s:%d", k, counts[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// EnvDisabled reads SACR_DISABLE_SCANNERS from the process environment.
// A thin helper so cmd/sacr doesn't need to know the env-var name.
func EnvDisabled() map[string]struct{} {
	return ParseDisabled(os.Getenv("SACR_DISABLE_SCANNERS"))
}
