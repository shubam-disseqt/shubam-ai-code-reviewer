// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scanner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
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
	Module   string       `json:"module"`
	Package  string       `json:"package"`
	Function string       `json:"function"`
	Position *govulnPos   `json:"position,omitempty"`
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

	//nolint:gosec // args are all constants
	cmd := exec.CommandContext(ctx, "govulncheck", "-json", "./...")
	cmd.Dir = repoRoot
	cmd.Stderr = s.stderr
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		// govulncheck exits non-zero when it finds vulns AND when its
		// own resolver fails. We can't distinguish those from the exit
		// code alone, so we parse whatever it emitted before returning.
		findings, perr := parseGovulncheck(stdout.Bytes(), repoRoot)
		if perr != nil || len(findings) == 0 {
			return nil, fmt.Errorf("govulncheck exec: %w", err)
		}
		return findings, nil
	}
	return parseGovulncheck(stdout.Bytes(), repoRoot)
}

// parseGovulncheck consumes NDJSON emitted by `govulncheck -json` and
// produces the normalised finding list. Broken lines are skipped, not
// fatal — govulncheck's streaming shape means a partial capture (e.g.
// process killed) can still yield useful data.
func parseGovulncheck(data []byte, repoRoot string) ([]ScannerFinding, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// govulncheck emits object-per-line; a couple of lines can be tens of
	// KB when the OSV details block is fat. Give the scanner room.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	osvByID := make(map[string]*govulnOSV)
	var findings []*govulnFinding
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg govulncheckMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// A single malformed line is skipped, not fatal.
			continue
		}
		if msg.OSV != nil {
			osvByID[msg.OSV.ID] = msg.OSV
		}
		if msg.Finding != nil {
			findings = append(findings, msg.Finding)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan output: %w", err)
	}

	out := make([]ScannerFinding, 0, len(findings))
	for _, f := range findings {
		step, ok := userCodeStep(f.Trace, repoRoot)
		if !ok {
			// Not reachable from user code — govulncheck emits these
			// too, but they're low-signal in a code-review context.
			continue
		}
		desc := f.OSV
		if meta := osvByID[f.OSV]; meta != nil {
			desc = firstNonEmpty(meta.Summary, meta.ID)
		}
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

// userCodeStep returns the first trace step whose position filename sits
// under repoRoot. govulncheck traces start at the user's call site and
// descend into the vulnerable stdlib/module function; we want the top
// entry so the finding attaches to code the reviewer actually owns.
func userCodeStep(trace []govulnStep, repoRoot string) (govulnStep, bool) {
	for _, s := range trace {
		if s.Position == nil || s.Position.Filename == "" {
			continue
		}
		if repoRoot == "" || strings.HasPrefix(s.Position.Filename, repoRoot) {
			return s, true
		}
	}
	return govulnStep{}, false
}
