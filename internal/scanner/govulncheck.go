// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// govulncheckScanner runs `govulncheck -json ./...` in repoRoot and parses
// the streaming NDJSON on stdout. Each line is a JSON object with exactly
// one of the following top-level keys: config, progress, osv, finding. We
// only surface `finding` entries whose trace reaches user code.
type govulncheckScanner struct {
	stderr io.Writer
}

func (s *govulncheckScanner) Name() string { return "govulncheck" }

// govulncheckMessage is the streaming NDJSON envelope. Each incoming line
// populates exactly one of these pointers; the rest are nil.
type govulncheckMessage struct {
	OSV     *govulnOSV     `json:"osv,omitempty"`
	Finding *govulnFinding `json:"finding,omitempty"`
}

// govulnOSV is the vulnerability metadata block (OSV format).
type govulnOSV struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
	Details string `json:"details"`
}

// govulnFinding is one traceback for a specific vulnerability.
type govulnFinding struct {
	OSV          string       `json:"osv"`
	FixedVersion string       `json:"fixed_version"`
	Trace        []govulnStep `json:"trace"`
}

type govulnStep struct {
	Module   string     `json:"module"`
	Package  string     `json:"package"`
	Function string     `json:"function"`
	Position *govulnPos `json:"position,omitempty"`
}

type govulnPos struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
	Offset   int    `json:"offset"`
}

func (s *govulncheckScanner) Run(ctx context.Context, repoRoot string, _ []string) ([]ScannerFinding, error) {
	if _, err := exec.LookPath("govulncheck"); err != nil {
		return nil, fmt.Errorf("skipping govulncheck (not installed)")
	}

	// govulncheck exits non-zero when it finds vulns AND when its own
	// resolver fails. We treat "cmd failed AND no parseable findings" as a
	// transient error worth retrying; every other outcome (success, or
	// non-zero with real findings) short-circuits the retry loop.
	var stdout bytes.Buffer
	var findings []ScannerFinding
	err := execAttempt(ctx, func() error {
		stdout.Reset()
		//nolint:gosec // args are all constants
		cmd := exec.CommandContext(ctx, "govulncheck", "-json", "./...")
		cmd.Dir = repoRoot
		cmd.Stderr = s.stderr
		cmd.Stdout = &stdout
		runErr := cmd.Run()
		parsed, perr := parseGovulncheck(stdout.Bytes(), repoRoot)
		if runErr == nil {
			findings = parsed
			return perr
		}
		if perr != nil || len(parsed) == 0 {
			return runErr
		}
		// Non-zero exit + real findings = expected vulns, keep them.
		findings = parsed
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("govulncheck exec: %w", err)
	}
	return findings, nil
}

// parseGovulncheck consumes NDJSON emitted by `govulncheck -json` and
// produces the normalised finding list. Broken lines are skipped, not
// fatal — govulncheck's streaming shape means a partial capture (e.g.
// process killed) can still yield useful data.
func parseGovulncheck(data []byte, repoRoot string) ([]ScannerFinding, error) {
	// govulncheck -json pretty-prints every object across many lines, so a
	// line scanner never sees a complete document. Stream-decode instead;
	// this also accepts the compact one-object-per-line shape.
	osvByID := make(map[string]*govulnOSV)
	var findings []*govulnFinding
	// Stream-decode; on a malformed object skip to the next line and resume
	// so one bad record (or a truncated capture) does not discard the rest.
	pos := 0
	for pos < len(data) {
		dec := json.NewDecoder(bytes.NewReader(data[pos:]))
		var msg govulncheckMessage
		err := dec.Decode(&msg)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			pos += int(dec.InputOffset())
			if nl := bytes.IndexByte(data[pos:], '\n'); nl >= 0 {
				pos += nl + 1
			} else {
				break
			}
			continue
		}
		pos += int(dec.InputOffset())
		if msg.OSV != nil {
			osvByID[msg.OSV.ID] = msg.OSV
		}
		if msg.Finding != nil {
			findings = append(findings, msg.Finding)
		}
	}

	mainModule := readModulePath(repoRoot)
	seen := make(map[string]struct{}, len(findings))
	out := make([]ScannerFinding, 0, len(findings))
	for _, f := range findings {
		step, ok := userCodeStep(f.Trace, repoRoot, mainModule)
		if !ok {
			// Not reachable from user code — govulncheck emits these
			// too, but they're low-signal in a code-review context.
			continue
		}
		desc := f.OSV
		if meta := osvByID[f.OSV]; meta != nil {
			desc = firstNonEmpty(meta.Summary, meta.ID)
		}
		key := f.OSV + "|" + step.Position.Filename + "|" + strconv.Itoa(step.Position.Line)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ScannerFinding{
			Tool:        "govulncheck",
			RuleID:      f.OSV,
			Path:        normalisePath(step.Position.Filename, repoRoot),
			Line:        step.Position.Line,
			Kind:        KindCVE,
			Severity:    SeverityHigh,
			Description: desc,
			Evidence:    strings.TrimSpace(step.Package + "." + step.Function),
		})
	}
	return out, nil
}

// userCodeStep returns the trace step that belongs to the reviewed module.
// govulncheck reports user frames with repo-relative filenames and stdlib
// frames as "src/...", so a path-prefix test against repoRoot never
// matches; match on the module path from go.mod instead. Without a go.mod
// fall back to "relative and not stdlib", then to the prefix test.
func userCodeStep(trace []govulnStep, repoRoot, mainModule string) (govulnStep, bool) {
	for _, s := range trace {
		if s.Position == nil || s.Position.Filename == "" {
			continue
		}
		fn := s.Position.Filename
		if mainModule != "" {
			if s.Module == mainModule {
				return s, true
			}
			continue
		}
		// A leading slash counts as absolute on Windows too, so unix
		// module-cache paths in fixtures never look "relative" there.
		isAbs := filepath.IsAbs(fn) || strings.HasPrefix(fn, "/")
		if (!isAbs && !strings.HasPrefix(fn, "src/")) || strings.HasPrefix(fn, repoRoot) {
			return s, true
		}
	}
	return govulnStep{}, false
}

// readModulePath returns the `module` line of <repoRoot>/go.mod, or "".
func readModulePath(repoRoot string) string {
	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
