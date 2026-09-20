// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package scoring

import (
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// suggestionCategories lists the LlmComment.Category values that are
// treated as proactive, non-blocking code-quality suggestions when paired
// with severity=low and a non-empty suggestion_code.
var suggestionCategories = map[string]struct{}{
	"style":           {},
	"maintainability": {},
	"test":            {},
	"documentation":   {},
}

// IsSuggestion reports whether a comment is a proactive code-quality
// suggestion. Suggestions are INFO-ONLY by default and do not fail CI
// unless the operator opts in via FilterBlocking's suggestionsBlocking flag.
//
// A comment qualifies when all three hold:
//   - severity == "low"
//   - category is one of {style, maintainability, test, documentation}
//   - suggestion_code is non-empty (i.e. the LLM produced an actionable diff)
func IsSuggestion(c model.LlmComment) bool {
	if strings.ToLower(strings.TrimSpace(c.Severity)) != "low" {
		return false
	}
	if strings.TrimSpace(c.SuggestionCode) == "" {
		return false
	}
	_, ok := suggestionCategories[strings.ToLower(strings.TrimSpace(c.Category))]
	return ok
}

// FilterBlocking returns the subset of comments that should count toward
// the CI-blocking exit code. When suggestionsBlocking is true the input is
// returned unchanged; otherwise IsSuggestion comments are dropped.
func FilterBlocking(comments []model.LlmComment, suggestionsBlocking bool) []model.LlmComment {
	if suggestionsBlocking {
		return comments
	}
	out := make([]model.LlmComment, 0, len(comments))
	for _, c := range comments {
		if IsSuggestion(c) {
			continue
		}
		out = append(out, c)
	}
	return out
}
