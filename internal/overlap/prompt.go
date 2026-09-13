// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/llm/prompts/overlap.py
// under Apache License 2.0.

package overlap

import (
	"strconv"
	"strings"

	"github.com/shubam-disseqt/z-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
)

const (
	// maxBodyChars caps candidate body length in the prompt. Titles + the
	// description lede carry the intent; the full body rarely adds signal
	// and blows up tokens on chatty PRs.
	maxBodyChars = 600
	// Per Mira: current PR files capped at 50, candidate PR files at 20.
	maxCurrentFiles   = 50
	maxCandidateFiles = 20
)

// systemPrompt is the model's instruction. Kept in one string constant so a
// prompt-eval diff is easy to read.
const systemPrompt = `You are a code-review assistant judging whether two open pull requests are stepping on each other. For each candidate PR, decide its relationship to the PR under review and classify it as one of:
- "merge_conflict": they edit the same code, so merging both will collide or one will silently clobber the other.
- "duplicate_effort": they pursue the same goal or implement the same feature/fix, even if via different files — redundant work.
- "both": both of the above.
- "none": no meaningful overlap; the shared files are incidental (e.g. both bump the same lockfile or touch an unrelated shared index).

Be conservative: prefer "none" unless the overlap is real and worth a reviewer's attention. Reason from the titles, descriptions, and shared files — do not invent details you cannot see.

Respond with ONLY a JSON object of this exact shape:
{"overlaps": [{"pr_number": <int>, "kind": "merge_conflict|duplicate_effort|both|none", "reason": "<one concise sentence>", "confidence": <0.0-1.0>}]}
Include one entry for every candidate PR.`

// candidate bundles a survivor of the pre-filter — the ref, its fingerprint,
// and the files it shares with the current PR (empty if selected on
// title-similarity alone).
type candidate struct {
	Ref    gh.OpenPRRef
	FP     fingerprint
	Shared []string
}

// buildOverlapPrompt renders the system + user messages for the LLM verdict.
// Mira builds a JSON envelope; we render markdown-ish plain text to match
// the reference exactly — the model gets the same input on both sides.
func buildOverlapPrompt(current PR, cands []candidate) llm.ChatRequest {
	var b strings.Builder
	b.WriteString("## PR under review — #")
	b.WriteString(strconv.Itoa(current.Number))
	b.WriteString(": ")
	b.WriteString(current.Title)
	b.WriteByte('\n')
	if body := truncate(current.Body, maxBodyChars); body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString("Files changed:\n")
	for _, p := range takeStrings(current.Paths, maxCurrentFiles) {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString("## Candidate open PRs")
	for _, c := range cands {
		b.WriteString("\n\n### PR #")
		b.WriteString(strconv.Itoa(c.Ref.Number))
		b.WriteString(": ")
		b.WriteString(c.FP.Title)
		b.WriteByte('\n')
		if body := truncate(c.FP.Body, maxBodyChars); body != "" {
			b.WriteString(body)
			b.WriteByte('\n')
		}
		if len(c.Shared) > 0 {
			b.WriteString("Files shared with the PR under review: ")
			b.WriteString(strings.Join(takeStrings(c.Shared, maxCandidateFiles), ", "))
			b.WriteByte('\n')
		} else {
			b.WriteString("Files shared with the PR under review: none\n")
			if len(c.FP.Paths) > 0 {
				b.WriteString("Its changed files: ")
				b.WriteString(strings.Join(takeStrings(c.FP.Paths, maxCandidateFiles), ", "))
				b.WriteByte('\n')
			}
		}
	}

	// Nudge the model toward deterministic-ish output. Temperature 0.0 is a
	// deliberate corner-cut for classification; the prompt is closed-form.
	temp := 0.0
	return llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: b.String()},
		},
		Temperature: &temp,
	}
}

// truncate lifts Mira's _truncate: trim, hard-cap at limit, append ellipsis.
func truncate(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return strings.TrimRight(s[:limit], " \t\n\r") + "…"
}

func takeStrings(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return xs[:n]
}
