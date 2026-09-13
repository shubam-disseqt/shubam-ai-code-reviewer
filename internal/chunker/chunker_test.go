// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package chunker

import (
	"reflect"
	"testing"
)

func TestChunk(t *testing.T) {
	a := Item{Path: "a", TokenCount: 30}
	b := Item{Path: "b", TokenCount: 30}
	c := Item{Path: "c", TokenCount: 30}
	big := Item{Path: "big", TokenCount: 500}

	tests := []struct {
		name  string
		items []Item
		limit int
		want  [][]Item
	}{
		{
			name:  "zero items returns nil",
			items: nil,
			limit: 100,
			want:  nil,
		},
		{
			name:  "one oversized item gets own chunk",
			items: []Item{big},
			limit: 100,
			want:  [][]Item{{big}},
		},
		{
			name:  "n items fitting in one chunk",
			items: []Item{a, b, c},
			limit: 100,
			want:  [][]Item{{a, b, c}},
		},
		{
			name:  "n items requiring two chunks preserves order",
			items: []Item{a, b, c},
			limit: 60,
			want:  [][]Item{{a, b}, {c}},
		},
		{
			name:  "n items requiring three chunks",
			items: []Item{a, b, c},
			limit: 30,
			want:  [][]Item{{a}, {b}, {c}},
		},
		{
			name:  "mixed: oversized between fits",
			items: []Item{a, big, b},
			limit: 100,
			want:  [][]Item{{a, b}, {big}},
		},
		{
			name:  "exact fit boundary",
			items: []Item{a, b},
			limit: 60,
			want:  [][]Item{{a, b}},
		},
		{
			name:  "non-positive limit sends each item to its own chunk",
			items: []Item{a, b},
			limit: 0,
			want:  [][]Item{{a}, {b}},
		},
		{
			name:  "negative limit sends each item to its own chunk",
			items: []Item{a, b},
			limit: -5,
			want:  [][]Item{{a}, {b}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Chunk(tc.items, tc.limit)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Chunk() = %v; want %v", got, tc.want)
			}
		})
	}
}

// TestChunk_FirstFit verifies the "earlier chunk with room wins" behavior:
// item 3 (small) should slot into chunk 0 rather than chunk 1.
func TestChunk_FirstFit(t *testing.T) {
	items := []Item{
		{Path: "a", TokenCount: 40}, // chunk 0
		{Path: "b", TokenCount: 90}, // chunk 1 (a+b > 100)
		{Path: "c", TokenCount: 30}, // fits back into chunk 0 (40+30 <= 100)
	}
	got := Chunk(items, 100)
	want := [][]Item{
		{items[0], items[2]},
		{items[1]},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Chunk() = %v; want %v", got, want)
	}
}
