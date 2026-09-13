// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt — adapted from alibaba/open-code-review
// Adapted from alibaba/open-code-review internal/diff/relocation.go

package diff

import "strings"

// extractCodeBlock extracts the content of the first fenced code block from text.
// Returns empty string if no code block is found.
func extractCodeBlock(text string) string {
	text = strings.TrimSpace(text)
	start := strings.Index(text, "```")
	if start < 0 {
		return ""
	}
	afterOpen := start + 3
	// Skip optional language tag on the opening fence line.
	if nl := strings.IndexByte(text[afterOpen:], '\n'); nl >= 0 {
		afterOpen += nl + 1
	} else {
		return ""
	}
	end := strings.Index(text[afterOpen:], "```")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(text[afterOpen : afterOpen+end])
}
