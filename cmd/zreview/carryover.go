// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"fmt"
	"io"

	"github.com/shubam-disseqt/z-code-reviewer/internal/findings"
	"github.com/shubam-disseqt/z-code-reviewer/internal/fingerprint"
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
)

// carryoverResult is the payload the emitter needs from stage 11.5:
// a list of comments (fresh + carried) with a parallel state map so
// downstream posting can update rather than duplicate.
type carryoverResult struct {
	Comments []model.LlmComment
	// State by comment fingerprint. Empty ⇒ carry-over disabled (no PR id).
	State map[string]findings.State
}

// runCarryover reconciles fresh review comments against persisted findings
// for (owner, repo, pr) and returns the merged comment stream. Best-effort:
// any failure returns the fresh comments unchanged and logs to `out`.
//
// Skipped when pr == 0 (workspace / local mode has no stable identity).
func runCarryover(comments []model.LlmComment, changedPaths []string, owner, repo string, pr int, out io.Writer) carryoverResult {
	res := carryoverResult{Comments: comments}
	if pr == 0 || owner == "" || repo == "" {
		return res
	}

	dir, err := findings.DefaultDir()
	if err != nil {
		fmt.Fprintf(out, "[zreview] findings: %v (skipping carry-over)\n", err)
		return res
	}
	previous, err := findings.Load(dir, owner, repo, pr)
	if err != nil {
		fmt.Fprintf(out, "[zreview] findings: %v (skipping carry-over)\n", err)
		return res
	}

	fresh := make([]findings.Finding, 0, len(comments))
	for _, c := range comments {
		fp := fingerprint.Fingerprint(fingerprint.Input{
			Owner:    owner,
			Repo:     repo,
			Category: c.Category,
			Path:     c.Path,
			Symbol:   symbolFor(c),
			Snippet:  c.ExistingCode,
		})
		fresh = append(fresh, findings.Finding{
			Fingerprint: fp,
			Comment:     c,
		})
	}

	reconciled := findings.Reconcile(previous, fresh, changedPaths)
	counts := findings.Summarize(previous, reconciled)

	if err := findings.Save(dir, owner, repo, pr, reconciled); err != nil {
		fmt.Fprintf(out, "[zreview] findings: save: %v\n", err)
	}

	fmt.Fprintf(out, "[zreview] findings: %d carried, %d resolved, %d new\n",
		counts.Carried, counts.Resolved, counts.New)

	// Rebuild the comment stream from the reconciled set, including carried
	// comments so a downstream GitHub poster can decide to update.
	res.Comments = make([]model.LlmComment, 0, len(reconciled))
	res.State = make(map[string]findings.State, len(reconciled))
	for _, f := range reconciled {
		res.Comments = append(res.Comments, f.Comment)
		res.State[f.Fingerprint] = f.State
	}
	return res
}

// symbolFor returns a stable per-comment symbol. LlmComment doesn't carry
// one directly; fall back to the resolved line range so different findings
// on the same file at different sites don't collide.
func symbolFor(c model.LlmComment) string {
	if c.StartLine == 0 && c.EndLine == 0 {
		return ""
	}
	return fmt.Sprintf("L%d-%d", c.StartLine, c.EndLine)
}

// commentFingerprint recomputes the fingerprint for a single comment. Kept
// public within the package so the emitter can key state without re-running
// the whole reconciliation.
func commentFingerprint(owner, repo string, c model.LlmComment) string {
	return fingerprint.Fingerprint(fingerprint.Input{
		Owner:    owner,
		Repo:     repo,
		Category: c.Category,
		Path:     c.Path,
		Symbol:   symbolFor(c),
		Snippet:  c.ExistingCode,
	})
}
