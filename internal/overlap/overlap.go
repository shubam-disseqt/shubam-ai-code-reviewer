// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/core/overlap.py
// under Apache License 2.0.

package overlap

import (
	"context"
	"log"
	"sort"

	"github.com/shubam-disseqt/z-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
)

// Detect runs the cross-PR overlap pipeline: list open PRs → pre-filter →
// batched LLM verdict → confidence filter → sort. Any failure returns
// ([], nil) — overlap detection is best-effort and must never block review.
//
// ponytail: add fingerprint cache when overlap is called for many PRs per
// session. Right now we re-fetch every candidate's file list on every call,
// which is fine for the CLI review-one-PR path but wasteful in a
// long-running batch review.
func Detect(ctx context.Context, cfg Config, cur PR, ghc *gh.Client, llmc llm.LLMClient) ([]Finding, error) {
	if !cfg.Enabled || ghc == nil || llmc == nil {
		return nil, nil
	}
	limit := cfg.MaxCandidates
	if limit <= 0 {
		limit = DefaultConfig().MaxCandidates
	}

	// Stage 0 — enumerate open PRs. Excess-fetch by 1 so we can drop the
	// current PR and still hit the cap.
	refs, err := ghc.ListOpenPRs(ctx, cur.Owner, cur.Repo, limit+1)
	if err != nil {
		log.Printf("overlap: list open PRs failed, skipping: %v", err)
		return nil, nil
	}

	// Stage 1 — resolve fingerprints + run the deterministic pre-filter.
	survivors := make([]candidate, 0, len(refs))
	for _, ref := range refs {
		if !acceptRef(cur, ref) {
			continue
		}
		fp := fingerprint{
			Number:  ref.Number,
			HeadSHA: ref.HeadSHA,
			Title:   ref.Title,
			Body:    ref.Body,
		}
		paths, err := ghc.GetPRFiles(ctx, cur.Owner, cur.Repo, ref.Number, 300)
		if err != nil {
			// Never derail the batch — log and skip this candidate.
			log.Printf("overlap: get_pr_files failed for #%d: %v", ref.Number, err)
			continue
		}
		fp.Paths = paths
		keep, shared := prefilter(cur, fp, cfg.TitleSimilarityThreshold)
		if !keep {
			continue
		}
		survivors = append(survivors, candidate{Ref: ref, FP: fp, Shared: shared})
	}

	if len(survivors) == 0 {
		return nil, nil
	}

	// Stage 2 — one batched LLM judgment over the shortlist.
	req := buildOverlapPrompt(cur, survivors)
	resp, err := llmc.CompletionsWithCtx(ctx, req)
	if err != nil {
		log.Printf("overlap: LLM judgment failed, skipping: %v", err)
		return nil, nil
	}
	verdicts := parseVerdict(resp.Content())

	// Stage 3 — filter by confidence + kind, hydrate into Finding.
	out := make([]Finding, 0, len(survivors))
	for _, c := range survivors {
		v, ok := verdicts[c.Ref.Number]
		if !ok {
			continue
		}
		if v.Kind == "none" || v.Conf < cfg.ConfidenceFloor {
			continue
		}
		out = append(out, Finding{
			Number:      c.Ref.Number,
			HTMLURL:     c.Ref.HTMLURL,
			Title:       c.FP.Title,
			Kind:        v.Kind,
			Confidence:  v.Conf,
			Reason:      v.Reason,
			SharedFiles: c.Shared,
		})
	}

	// Sort stable by (kindRank, confidence) descending.
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := kindRank[out[i].Kind], kindRank[out[j].Kind]
		if ri != rj {
			return ri > rj
		}
		return out[i].Confidence > out[j].Confidence
	})
	return out, nil
}

// acceptRef drops candidates the model shouldn't waste tokens on: the current
// PR itself, drafts, bot authors, and branches in the same stack as `cur`.
func acceptRef(cur PR, ref gh.OpenPRRef) bool {
	if ref.Number == cur.Number {
		return false
	}
	if ref.IsDraft {
		return false
	}
	if normalizeLogin(ref.UserLogin) != "" && looksLikeBot(ref.UserLogin) {
		return false
	}
	if isStacked(cur, ref) {
		return false
	}
	return true
}

// looksLikeBot is a light heuristic — a GitHub App login carries the
// "[bot]" suffix. Real users can't register logins containing "[".
func looksLikeBot(login string) bool {
	if login == "" {
		return false
	}
	// GitHub Apps: exact "<name>[bot]" — never a substring elsewhere in a
	// real login.
	if len(login) > 5 && login[len(login)-5:] == "[bot]" {
		return true
	}
	return false
}
