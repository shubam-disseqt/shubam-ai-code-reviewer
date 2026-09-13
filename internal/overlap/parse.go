// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/core/overlap.py
// under Apache License 2.0.

package overlap

import (
	"encoding/json"
	"regexp"
	"strings"
)

// verdict is the parsed per-candidate judgment from the LLM.
type verdict struct {
	Kind   string
	Reason string
	Conf   float64
}

// validKinds is the closed set from the prompt. Anything else clamps to
// "none" — the caller then drops it.
var validKinds = map[string]struct{}{
	"merge_conflict":   {},
	"duplicate_effort": {},
	"both":             {},
	"none":             {},
}

// thinkBlock strips <think>...</think> that some reasoning models emit
// before the JSON payload.
var thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)

// parseVerdict is tolerant by design — one malformed entry never nukes the
// whole batch. Matches Mira._parse_overlap_response.
func parseVerdict(raw string) map[int]verdict {
	cleaned := stripCodeFences(thinkBlock.ReplaceAllString(raw, ""))
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return map[int]verdict{}
	}

	// Decode into a loose shape; we validate per-entry below.
	var envelope struct {
		Overlaps []json.RawMessage `json:"overlaps"`
	}
	if err := json.Unmarshal([]byte(cleaned), &envelope); err != nil {
		return map[int]verdict{}
	}

	out := make(map[int]verdict, len(envelope.Overlaps))
	for _, raw := range envelope.Overlaps {
		var entry struct {
			PRNumber   json.Number `json:"pr_number"`
			Kind       string      `json:"kind"`
			Reason     string      `json:"reason"`
			Confidence json.Number `json:"confidence"`
		}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if err := dec.Decode(&entry); err != nil {
			continue
		}
		n, err := entry.PRNumber.Int64()
		if err != nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(entry.Kind))
		if _, ok := validKinds[kind]; !ok {
			kind = "none"
		}
		conf, err := entry.Confidence.Float64()
		if err != nil {
			conf = 0.0
		}
		if conf < 0 {
			conf = 0
		}
		if conf > 1 {
			conf = 1
		}
		out[int(n)] = verdict{
			Kind:   kind,
			Reason: strings.TrimSpace(entry.Reason),
			Conf:   conf,
		}
	}
	return out
}

// stripCodeFences removes a leading ```...``` fence (with or without a
// language tag) and any trailing ```. Matches mira.llm.utils.strip_code_fences
// well enough for this call — we only need JSON out.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence line entirely (```json\n or just ```\n).
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	} else {
		s = s[3:]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
