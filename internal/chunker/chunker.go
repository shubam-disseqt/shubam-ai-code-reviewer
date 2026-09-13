// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/core/chunker.py under Apache License 2.0.

// Package chunker groups file diffs into token-bounded batches using a
// first-fit strategy that preserves input order.
package chunker

// Item is a single file diff to be chunked.
type Item struct {
	Path       string
	Diff       string
	TokenCount int
}

// Chunk groups items into slices whose combined TokenCount does not exceed
// tokenLimit, using first-fit in the caller's input order.
//
// A single item whose TokenCount exceeds tokenLimit is placed in a chunk by
// itself (semantics: "oversized item gets its own chunk"). tokenLimit <= 0 is
// treated the same way — every item ends up in its own chunk.
//
// Ordering guarantee: within any returned chunk, items appear in the same
// relative order as they did in the input.
func Chunk(items []Item, tokenLimit int) [][]Item {
	if len(items) == 0 {
		return nil
	}

	var chunks [][]Item
	var totals []int

	for _, it := range items {
		// Oversized item (or non-positive limit): standalone chunk.
		if tokenLimit <= 0 || it.TokenCount > tokenLimit {
			chunks = append(chunks, []Item{it})
			totals = append(totals, it.TokenCount)
			continue
		}

		// First-fit: earliest chunk with room wins. Preserves input order
		// because we append to the chosen chunk instead of reordering.
		placed := false
		for i := range chunks {
			if totals[i]+it.TokenCount <= tokenLimit {
				chunks[i] = append(chunks[i], it)
				totals[i] += it.TokenCount
				placed = true
				break
			}
		}
		if !placed {
			chunks = append(chunks, []Item{it})
			totals = append(totals, it.TokenCount)
		}
	}

	return chunks
}
