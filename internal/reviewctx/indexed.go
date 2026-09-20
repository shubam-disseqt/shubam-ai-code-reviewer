// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/context.py under
// Apache License 2.0.

package reviewctx

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/index"
)

// trivialSummaryFloor mirrors Mira's <600 byte "trivial file" rule: summaries
// derived from very small files are stored empty by the indexer. When we hit
// such a summary here we skip it — the prompt would carry no signal.
const trivialSummaryFloor = 0 // length check: len(strings.TrimSpace(s.Summary)) > 0

// BuildIndexed pulls context from the Store. Token budget is split
// 60% source excerpts / 30% file summaries / 10% directory context.
//
// The Store may return errors on any call — we degrade gracefully: a failed
// blast-radius query means "no blast section", not "abort". Only an aborted
// context surfaces (via ctx.Err()).
func BuildIndexed(ctx context.Context, opts Options) (string, error) {
	o := opts.resolved()
	if o.Store == nil {
		return "", errors.New("reviewctx: Store required for indexed mode")
	}
	if len(o.ChangedPaths) == 0 || o.TokenBudget <= 0 {
		return "", nil
	}

	charBudget := tokensToChars(o.TokenBudget)
	// 60/30/10 split.
	sourceBudget := charBudget * 60 / 100
	summaryBudget := charBudget * 30 / 100
	dirBudget := charBudget * 10 / 100

	// Rank changed paths by inbound-edge count so the most-depended-on files
	// win the source-excerpt budget first.
	changed := rankByInbound(ctx, o.Store, o.ChangedPaths)

	// --- Blast radius (deduplicated). Same list feeds the source section
	//     AND the dedicated blast-radius section.
	blast, _ := o.Store.GetBlastRadius(ctx, changed)
	blast = dedupeBlastRadius(blast)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	blastByPath := make(map[string]index.BlastRadiusEntry, len(blast))
	for _, e := range blast {
		blastByPath[e.Path] = e
	}

	// --- Source excerpts (60%) ---
	sourceSection, filesWithSource := renderSourceSection(
		changed, blast, o.NewFileContent, sourceBudget,
	)

	// --- Directory context (10%) ---
	dirSection := renderDirSection(ctx, o.Store, changed, dirBudget)

	// --- File summaries (30%) ---
	summarySection := renderSummarySection(
		ctx, o.Store, changed, filesWithSource, summaryBudget,
	)

	// --- Blast radius list (from summary budget's remainder) ---
	blastSection := renderBlastSection(blast, filesWithSource)

	// Compose.
	var b strings.Builder
	b.WriteString("## Codebase context\n\n")
	written := len(b.String())
	for _, sec := range []string{dirSection, sourceSection, summarySection, blastSection} {
		if sec == "" {
			continue
		}
		b.WriteString(sec)
		if !strings.HasSuffix(sec, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	out := b.String()
	if strings.TrimSpace(out[written:]) == "" {
		return "", nil
	}
	// Belt-and-braces overall cap: if any section blew its slice we still
	// want to respect the caller's total budget.
	if len(out) > charBudget {
		out = out[:charBudget]
		if idx := strings.LastIndex(out, "\n"); idx > 0 {
			out = out[:idx]
		}
		out += "\n\n*(codebase context truncated to fit token budget)*"
	}
	return out, nil
}

// rankByInbound sorts changed by inbound-edge count desc (most-imported first).
// A Store error leaves the original order intact.
func rankByInbound(ctx context.Context, store index.Store, changed []string) []string {
	counts, err := store.GetInboundEdgeCounts(ctx, changed)
	if err != nil {
		return changed
	}
	out := append([]string(nil), changed...)
	sort.SliceStable(out, func(i, j int) bool { return counts[out[i]] > counts[out[j]] })
	return out
}

// renderSourceSection walks changed paths (then blast-radius paths, most
// impacted first) and inlines file content until sourceBudget is exhausted.
// Returns the section markdown plus the set of paths whose source was
// actually inlined (used downstream to avoid duplicating them as summaries).
func renderSourceSection(
	changed []string,
	blast []index.BlastRadiusEntry,
	newFileContent map[string]string,
	sourceBudget int,
) (string, map[string]struct{}) {
	if sourceBudget <= 0 || len(newFileContent) == 0 {
		return "", nil
	}
	filesWithSource := make(map[string]struct{})
	var b strings.Builder
	b.WriteString("### Source excerpts\n\n")
	baseLen := b.Len()
	used := 0

	// Ordering: changed files first, then blast entries sorted by number of
	// affected symbols (most impacted first).
	order := append([]string(nil), changed...)
	blastSorted := append([]index.BlastRadiusEntry(nil), blast...)
	sort.SliceStable(blastSorted, func(i, j int) bool {
		return len(blastSorted[i].AffectedSymbols) > len(blastSorted[j].AffectedSymbols)
	})
	seen := makeSet(order)
	for _, e := range blastSorted {
		if _, ok := seen[e.Path]; ok {
			continue
		}
		seen[e.Path] = struct{}{}
		order = append(order, e.Path)
	}

	for _, p := range order {
		if used >= sourceBudget {
			break
		}
		src, ok := newFileContent[p]
		if !ok || src == "" {
			continue
		}
		lang := path.Ext(p)
		lang = strings.TrimPrefix(lang, ".")
		remaining := sourceBudget - used
		excerpt := trimExcerpt(src, remaining-256) // reserve room for the fence header
		if strings.TrimSpace(excerpt) == "" {
			continue
		}
		block := fmt.Sprintf("#### `%s`\n```%s\n%s\n```\n\n", p, lang, excerpt)
		if used+len(block) > sourceBudget {
			continue
		}
		b.WriteString(block)
		used += len(block)
		filesWithSource[p] = struct{}{}
	}

	if b.Len() == baseLen {
		return "", filesWithSource
	}
	return b.String(), filesWithSource
}

// renderDirSection asks the Store for directory summaries of every unique
// parent-of-changed-file directory. Skips directories the Store didn't return.
func renderDirSection(ctx context.Context, store index.Store, changed []string, budget int) string {
	if budget <= 0 {
		return ""
	}
	parents := make(map[string]struct{})
	for _, p := range changed {
		d := path.Dir(p)
		if d != "." && d != "" {
			parents[d] = struct{}{}
		}
	}
	if len(parents) == 0 {
		return ""
	}
	dirs := make([]string, 0, len(parents))
	for d := range parents {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	rows, err := store.ListDirectorySummaries(ctx, dirs)
	if err != nil || len(rows) == 0 {
		return ""
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })

	var b strings.Builder
	b.WriteString("### Directory context\n")
	for _, r := range rows {
		if strings.TrimSpace(r.Summary) == "" {
			continue
		}
		fmt.Fprintf(&b, "- `%s/`: %s (%d files)\n", r.Path, r.Summary, r.FileCount)
	}
	out := b.String()
	if strings.TrimSpace(strings.TrimPrefix(out, "### Directory context\n")) == "" {
		return ""
	}
	if len(out) > budget {
		out = out[:budget]
		if idx := strings.LastIndex(out, "\n"); idx > 0 {
			out = out[:idx]
		}
	}
	return out
}

// renderSummarySection lists changed-file summaries and one hop out (imports).
// Files already inlined as source are skipped to avoid duplication.
func renderSummarySection(
	ctx context.Context,
	store index.Store,
	changed []string,
	filesWithSource map[string]struct{},
	budget int,
) string {
	if budget <= 0 {
		return ""
	}
	changedSummaries, _ := store.ListSummaries(ctx, changed)
	byPath := make(map[string]index.FileSummary, len(changedSummaries))
	for _, s := range changedSummaries {
		byPath[s.Path] = s
	}

	var b strings.Builder
	b.WriteString("### File summaries\n")
	baseLen := b.Len()

	// Changed files (sorted for stability).
	sortedChanged := append([]string(nil), changed...)
	sort.Strings(sortedChanged)
	for _, p := range sortedChanged {
		if _, ok := filesWithSource[p]; ok {
			continue
		}
		fs, ok := byPath[p]
		if !ok || !hasContent(fs.Summary) {
			continue
		}
		writeSummaryLine(&b, fs)
	}

	// One-hop related files.
	imports := make(map[string]struct{})
	changedSet := makeSet(changed)
	for _, fs := range changedSummaries {
		for _, imp := range fs.Imports {
			if _, isChanged := changedSet[imp]; isChanged {
				continue
			}
			if _, isSrc := filesWithSource[imp]; isSrc {
				continue
			}
			imports[imp] = struct{}{}
		}
	}
	if len(imports) > 0 {
		relPaths := make([]string, 0, len(imports))
		for p := range imports {
			relPaths = append(relPaths, p)
		}
		sort.Strings(relPaths)
		relatedSummaries, _ := store.ListSummaries(ctx, relPaths)
		if len(relatedSummaries) > 0 {
			b.WriteString("\n**Related files (imported by changed files):**\n")
			sort.SliceStable(relatedSummaries, func(i, j int) bool {
				return relatedSummaries[i].Path < relatedSummaries[j].Path
			})
			for _, fs := range relatedSummaries {
				if !hasContent(fs.Summary) {
					continue
				}
				writeSummaryLine(&b, fs)
			}
		}
	}

	out := b.String()
	if b.Len() == baseLen {
		return ""
	}
	if len(out) > budget {
		out = out[:budget]
		if idx := strings.LastIndex(out, "\n"); idx > 0 {
			out = out[:idx]
		}
	}
	return out
}

// writeSummaryLine renders one file summary plus its symbols + imports.
func writeSummaryLine(b *strings.Builder, fs index.FileSummary) {
	fmt.Fprintf(b, "- `%s`: %s\n", fs.Path, fs.Summary)
	for _, sym := range fs.Symbols {
		sig := sym.Signature
		if sig == "" {
			sig = sym.Name
		}
		if sym.Description != "" {
			fmt.Fprintf(b, "  - `%s`: %s\n", sig, sym.Description)
		} else {
			fmt.Fprintf(b, "  - `%s`\n", sig)
		}
	}
	if len(fs.Imports) > 0 {
		fmt.Fprintf(b, "  - Imports: %s\n", strings.Join(fs.Imports, ", "))
	}
}

// hasContent returns true iff s has a non-whitespace character. Mira stores
// empty summaries for trivial (<600 B) files; we must not emit those as
// `[empty]` lines in the prompt.
func hasContent(s string) bool { return len(strings.TrimSpace(s)) > trivialSummaryFloor }

// renderBlastSection lists (path, symbols, depth) rows for the blast radius,
// skipping paths already inlined as source.
func renderBlastSection(
	blast []index.BlastRadiusEntry,
	filesWithSource map[string]struct{},
) string {
	if len(blast) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Blast radius\n")
	baseLen := b.Len()
	for _, e := range blast {
		if _, ok := filesWithSource[e.Path]; ok {
			continue
		}
		syms := formatSymbols(e.AffectedSymbols)
		fmt.Fprintf(&b, "- `%s` -> %s (depth %d)\n", e.Path, syms, e.Depth)
	}
	if b.Len() == baseLen {
		return ""
	}
	return b.String()
}

// formatSymbols renders a comma-joined list of `sym()` entries, or "(none)".
func formatSymbols(syms []string) string {
	if len(syms) == 0 {
		return "(none)"
	}
	parts := make([]string, len(syms))
	for i, s := range syms {
		parts[i] = "`" + s + "()`"
	}
	return strings.Join(parts, ", ")
}

// dedupeBlastRadius collapses (path, symbol) duplicates. Mira gotcha:
// LLM-generated symbol refs occasionally repeat, so the Store's blast-radius
// query can return the same effective entry twice. We keep the shallowest
// depth for each path and union the affected-symbol sets.
func dedupeBlastRadius(entries []index.BlastRadiusEntry) []index.BlastRadiusEntry {
	if len(entries) == 0 {
		return entries
	}
	// Preserve first-seen order of paths, but merge duplicates.
	type slot struct {
		idx     int
		seenSym map[string]struct{}
	}
	byPath := make(map[string]*slot, len(entries))
	var out []index.BlastRadiusEntry
	for _, e := range entries {
		s, ok := byPath[e.Path]
		if !ok {
			seen := make(map[string]struct{}, len(e.AffectedSymbols))
			syms := make([]string, 0, len(e.AffectedSymbols))
			for _, sym := range e.AffectedSymbols {
				if _, dup := seen[sym]; dup {
					continue
				}
				seen[sym] = struct{}{}
				syms = append(syms, sym)
			}
			out = append(out, index.BlastRadiusEntry{
				Path:            e.Path,
				Summary:         e.Summary,
				AffectedSymbols: syms,
				Depth:           e.Depth,
			})
			byPath[e.Path] = &slot{idx: len(out) - 1, seenSym: seen}
			continue
		}
		cur := &out[s.idx]
		if e.Depth < cur.Depth {
			cur.Depth = e.Depth
		}
		for _, sym := range e.AffectedSymbols {
			if _, dup := s.seenSym[sym]; dup {
				continue
			}
			s.seenSym[sym] = struct{}{}
			cur.AffectedSymbols = append(cur.AffectedSymbols, sym)
		}
	}
	return out
}
