// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/core/overlap.py
// under Apache License 2.0.

package overlap

import (
	"fmt"
	"strings"
)

// kindLabel maps the model's kind to the human-facing label in the
// walkthrough. Matches the shape in the agent-2 report §9.
var kindLabel = map[string]string{
	"merge_conflict":   "merge-conflict risk",
	"duplicate_effort": "duplicate effort",
	"both":             "duplicate effort + merge-conflict risk",
}

// maxSharedShown is how many shared files we list before collapsing to
// "+N more". Three keeps the line scannable in the walkthrough.
const maxSharedShown = 3

// Render produces the markdown block for the walkthrough. Empty input →
// empty string so the caller can just concat it in.
func Render(findings []Finding) string {
	if len(findings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("> **⚠️ Potential overlap with other open PRs** — these may be stepping on this one:\n>\n")
	for _, f := range findings {
		label, ok := kindLabel[f.Kind]
		if !ok {
			// Should never happen — Detect filters "none" out — but if the
			// caller mints a Finding manually, fall through with the raw kind
			// rather than dropping it silently.
			label = f.Kind
		}
		fmt.Fprintf(&b, "> - [#%d](%s) (%s) — %s", f.Number, f.HTMLURL, label, ensureTrailingPeriod(f.Reason))
		if len(f.SharedFiles) > 0 {
			shown := f.SharedFiles
			if len(shown) > maxSharedShown {
				shown = shown[:maxSharedShown]
			}
			b.WriteString(" Shared: ")
			b.WriteString(strings.Join(shown, ", "))
			if extra := len(f.SharedFiles) - maxSharedShown; extra > 0 {
				fmt.Fprintf(&b, " +%d more", extra)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// ensureTrailingPeriod adds a period if the reason doesn't already end in
// punctuation — the walkthrough reads better with a hard stop before "Shared:".
func ensureTrailingPeriod(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if last := s[len(s)-1]; last == '.' || last == '!' || last == '?' {
		return s
	}
	return s + "."
}
