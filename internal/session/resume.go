// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt
//
// Adapted from alibaba/open-code-review internal/session/resume.go under
// Apache License 2.0. Modifications: dropped manifest-gated reuse, sealed
// input identity, and scope validation; kept only the JSONL replay pass so
// --resume compiles. Filled in when the reviewer actually gains
// review_item_done records.

package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ResumeState is the replayed, read-only checkpoint index for one prior
// session run. Empty for now: no review_item_done records are written yet.
type ResumeState struct {
	Items map[string]ResumedItem
}

// ResumedItem is one recovered file-level checkpoint.
type ResumedItem struct {
	Fingerprint string
	Comments    string
}

// resumeLine is the subset of every JSONL record we look at during replay.
// Everything else is ignored so unknown record types cost nothing.
type resumeLine struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Comments    string `json:"comments"`
}

// LoadResumeState replays a prior session's JSONL and returns the index of
// completed items. Unparseable lines are dropped (partial-crash tolerance):
// with no manifest to arbitrate coverage, a re-review is safer than losing
// the whole index.
func LoadResumeState(sessionFilePath string) (*ResumeState, error) {
	f, err := os.Open(sessionFilePath)
	if err != nil {
		return nil, fmt.Errorf("open resume session %q: %w", sessionFilePath, err)
	}
	defer f.Close()

	state := &ResumeState{Items: make(map[string]ResumedItem)}
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			var rec resumeLine
			if err := json.Unmarshal(line, &rec); err == nil {
				switch rec.Type {
				case "review_item_done", "review_item_reused":
					if rec.Fingerprint != "" {
						state.Items[rec.Fingerprint] = ResumedItem{
							Fingerprint: rec.Fingerprint,
							Comments:    rec.Comments,
						}
					}
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read resume session %q: %w", sessionFilePath, readErr)
		}
	}
	return state, nil
}
