// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package session

import (
	"regexp"
	"strings"
)

// Redact scrubs common credential shapes from s and returns the result. It is a
// best-effort filter for the session write path — false positives are worse
// than false negatives here (a mangled review comment is louder than a missed
// token), so patterns are bounded and conservative. Not a substitute for good
// repo hygiene; see docs/session-log.html#redaction.
func Redact(s string) string {
	if s == "" {
		return s
	}
	// PEM must run first: it collapses a multi-line block into a single marker,
	// which prevents the per-line env matcher from mangling BEGIN/END headers.
	s = redactPEM(s)
	s = jwtRE.ReplaceAllString(s, "[REDACTED-JWT]")
	s = basicAuthRE.ReplaceAllString(s, "${1}[REDACTED]@")
	s = envAssignRE.ReplaceAllStringFunc(s, redactEnvAssign)
	s = prefixedTokenRE.ReplaceAllString(s, "[REDACTED]")
	return s
}

// envAssignRE matches KEY=value on its own line (or after whitespace) where
// KEY looks credential-shaped. The value is captured up to end-of-line, an
// unescaped quote-close, or whitespace after an unquoted value.
//
// Key shapes matched (case-insensitive):
//   - AWS_*_KEY, AWS_*_TOKEN, AWS_*_SECRET
//   - *_API_KEY, *_TOKEN, *_SECRET, *_PASSWORD
//   - bare PASSWORD, PRIVATE_KEY
//
// The leading `(?m)^[ \t]*` anchor keeps this from firing inside prose like
// "the token=abc trick" that lives mid-sentence.
var envAssignRE = regexp.MustCompile(
	`(?im)^[ \t]*[+\-]?[ \t]*(?:export\s+)?([A-Z][A-Z0-9_]*(?:_API_KEY|_TOKEN|_SECRET|_PASSWORD|_PRIVATE_KEY)|AWS_[A-Z0-9_]*_(?:KEY|TOKEN|SECRET)|PASSWORD|PRIVATE_KEY)\s*=\s*("[^"\n]*"|'[^'\n]*'|\S+)`,
)

// redactEnvAssign preserves the key and quote style but blanks the value.
// Empty or placeholder values ("", ”, "...", ...) are left alone — an empty
// example in docs shouldn't be replaced with a scarier-looking [REDACTED].
func redactEnvAssign(match string) string {
	loc := envAssignRE.FindStringSubmatchIndex(match)
	if loc == nil {
		return match
	}
	key := match[loc[2]:loc[3]]
	val := match[loc[4]:loc[5]]

	if isPlaceholderValue(val) {
		return match
	}

	prefix := match[:loc[2]]
	replacement := "[REDACTED]"
	switch {
	case strings.HasPrefix(val, `"`):
		replacement = `"[REDACTED]"`
	case strings.HasPrefix(val, `'`):
		replacement = `'[REDACTED]'`
	}
	return prefix + key + "=" + replacement
}

// isPlaceholderValue detects sample/placeholder values that should not be
// redacted: empty strings, ellipses, and single-word tokens like <value> or
// YOUR_KEY_HERE that clearly aren't real credentials.
func isPlaceholderValue(v string) bool {
	trimmed := strings.Trim(v, `"'`)
	if trimmed == "" {
		return true
	}
	if trimmed == "..." || trimmed == "…" {
		return true
	}
	if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") {
		return true
	}
	return false
}

// prefixedTokenRE catches well-known credential prefixes anywhere in the line.
// Bounded lengths keep incidental short identifiers (a variable literally
// named `sk` in a snippet) from getting flagged.
//
// Patterns:
//   - sk-ant-<...>   Anthropic
//   - sk-<...>       OpenAI (must not match sk-ant to avoid double-redaction)
//   - ghp_<...>      GitHub personal token
//   - github_pat_<...>
//   - xoxb-<...>     Slack bot token
//   - AKIA<16 upper alnum>  AWS access key ID
var prefixedTokenRE = regexp.MustCompile(
	`\b(?:sk-ant-[A-Za-z0-9_\-]{20,}|sk-[A-Za-z0-9]{20,}|ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xoxb-[A-Za-z0-9\-]{20,}|AKIA[0-9A-Z]{16})\b`,
)

// jwtRE catches the classic three-segment base64url JWT. Bounded low end
// (20 chars per segment) avoids matching `a.b.c` prose.
var jwtRE = regexp.MustCompile(
	`\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{20,}\.[A-Za-z0-9_\-]{20,}\b`,
)

// basicAuthRE catches user:pass@ inside http(s) URLs. Deliberately narrow so
// it doesn't fire on things like `git@github.com:owner/repo`.
var basicAuthRE = regexp.MustCompile(
	`(https?://)[^\s:/@]+:[^\s@]+@`,
)

// pemBlockRE matches a PEM block payload (BEGIN ... END). DOTALL so newlines
// inside the block are consumed.
var pemBlockRE = regexp.MustCompile(
	`(?s)(-----BEGIN [A-Z0-9 ]+-----).*?(-----END [A-Z0-9 ]+-----)`,
)

func redactPEM(s string) string {
	return pemBlockRE.ReplaceAllString(s, "$1\n[REDACTED-PEM]\n$2")
}
