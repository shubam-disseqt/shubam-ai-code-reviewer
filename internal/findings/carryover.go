// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package findings

import "time"

// Reconcile applies the incremental-review state machine to a previous +
// fresh finding set and returns the merged view. `changedPaths` is the
// file list the CURRENT diff touches — anything absent from it is treated
// as unchanged.
//
// State transitions:
//   - previous fp present in fresh                                → keep    (matched again this run)
//   - previous fp absent from fresh, file NOT in changedPaths     → carried (untouched file, carry forward)
//   - previous fp absent from fresh, file IS in changedPaths      → resolved (dropped from output)
//   - fresh fp absent from previous                               → new
//
// Resolved findings are NOT returned — they've been fixed and shouldn't
// re-surface. Callers that want an audit trail can diff previous vs the
// returned slice by fingerprint.
func Reconcile(previous, fresh []Finding, changedPaths []string) []Finding {
	changed := make(map[string]struct{}, len(changedPaths))
	for _, p := range changedPaths {
		changed[p] = struct{}{}
	}
	prevByFP := make(map[string]Finding, len(previous))
	for _, f := range previous {
		prevByFP[f.Fingerprint] = f
	}
	freshByFP := make(map[string]Finding, len(fresh))
	for _, f := range fresh {
		freshByFP[f.Fingerprint] = f
	}

	now := time.Now().UTC()
	out := make([]Finding, 0, len(fresh)+len(previous))

	// Walk fresh first: matches become "keep", brand-new become "new".
	for _, f := range fresh {
		if prev, ok := prevByFP[f.Fingerprint]; ok {
			f.State = StateKeep
			f.FirstSeen = prev.FirstSeen
			f.LastSeen = now
			out = append(out, f)
			continue
		}
		if f.FirstSeen.IsZero() {
			f.FirstSeen = now
		}
		f.LastSeen = now
		f.State = StateNew
		out = append(out, f)
	}

	// Walk previous for anything the fresh pass didn't cover.
	for _, p := range previous {
		if _, matched := freshByFP[p.Fingerprint]; matched {
			continue // handled above
		}
		if _, touched := changed[p.Comment.Path]; touched {
			// File was touched, no matching finding this round → fixed.
			continue // drop, resolved
		}
		p.State = StateCarried
		p.LastSeen = now
		out = append(out, p)
	}

	return out
}

// Counts summarizes a reconciled slice for logging. Cheap to compute and
// keeps the review_cmd stage log-line one place.
type Counts struct {
	Kept     int
	Carried  int
	New      int
	Resolved int
}

// Summarize returns a Counts derived from (previous, reconciled). Resolved =
// items that were in previous but not in reconciled.
func Summarize(previous, reconciled []Finding) Counts {
	var c Counts
	seen := make(map[string]struct{}, len(reconciled))
	for _, f := range reconciled {
		seen[f.Fingerprint] = struct{}{}
		switch f.State {
		case StateKeep:
			c.Kept++
		case StateCarried:
			c.Carried++
		case StateNew:
			c.New++
		}
	}
	for _, p := range previous {
		if _, ok := seen[p.Fingerprint]; !ok {
			c.Resolved++
		}
	}
	return c
}
