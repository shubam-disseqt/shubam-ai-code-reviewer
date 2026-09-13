// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package rules loads org-level review rules from a git-hosted rules
// repo, filters them by scope + path glob, and renders the injectable
// "## Custom Review Rules" block for the review prompt.
package rules

import (
	"log"
	"strings"
)

// maxRulesInPrompt caps how many rules are rendered into the prompt to
// keep the injected block bounded. Overflow is logged.
const maxRulesInPrompt = 15

// RenderForPrompt renders the selected rules into the "## Custom Review
// Rules" markdown block. Returns an empty string when there are no rules;
// the caller inserts nothing in that case.
//
// Format (verbatim shape from Mira's review.jinja2 template):
//
//	## Custom Review Rules
//
//	Follow these rules when reviewing code. They take priority over your default behavior:
//
//	### <Title>
//	<Body>
func RenderForPrompt(rules []Rule) string {
	if len(rules) == 0 {
		return ""
	}
	if len(rules) > maxRulesInPrompt {
		dropped := len(rules) - maxRulesInPrompt
		log.Printf("[rules] capping injected rules at %d (dropped %d)", maxRulesInPrompt, dropped)
		rules = rules[:maxRulesInPrompt]
	}

	var b strings.Builder
	b.WriteString("## Custom Review Rules\n\n")
	b.WriteString("Follow these rules when reviewing code. They take priority over your default behavior:\n\n")
	for _, r := range rules {
		b.WriteString("### ")
		b.WriteString(r.Title)
		b.WriteString("\n")
		b.WriteString(strings.TrimRight(r.Body, "\n"))
		b.WriteString("\n\n")
	}
	return b.String()
}
