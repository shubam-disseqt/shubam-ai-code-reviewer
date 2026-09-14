// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llmloop"
	"github.com/shubam-disseqt/z-code-reviewer/internal/model"
	"github.com/shubam-disseqt/z-code-reviewer/internal/prompts"
	"github.com/shubam-disseqt/z-code-reviewer/internal/tool"
)

// loadPrompt reads an embedded template by name.
func loadPrompt(name string) (string, error) {
	b, err := prompts.Templates.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("prompts: load %s: %w", name, err)
	}
	return string(b), nil
}

// renderUserPrompt fills the {{...}} placeholders in main_task_user.md.
// Empty strings substitute for anything the caller does not have.
func renderUserPrompt(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

// buildReviewMessages assembles the main-task conversation from the loaded
// templates, the rendered rules block, the codebase context and this file's
// diff. changeFiles is the JSON-encoded list of every diff filename in the
// current run (out-of-batch context for the LLM). knownIssues is the
// pre-rendered "## Known Issues (from static analysis)" block for the file
// under review; empty when no scanner findings applied.
func buildReviewMessages(sys, userTmpl, systemRule, reviewCtx, knownIssues, changeFiles, diffs string) []llm.Message {
	// The reviewctx block is not first-class in main_task_user.md, so prepend
	// it under the system message as a codebase-context brief. Empty is fine.
	systemContent := sys
	if reviewCtx != "" {
		systemContent = systemContent + "\n\n## Codebase Context\n\n" + reviewCtx
	}
	if knownIssues != "" {
		systemContent = systemContent + "\n\n" + knownIssues
	}

	user := renderUserPrompt(userTmpl, map[string]string{
		"change_files":             changeFiles,
		"diffs":                    diffs,
		"current_system_date_time": time.Now().Format(time.RFC3339),
		"requirement_background":   "",
		"system_rule":              systemRule,
		"plan_guidance":            "",
		"confirmed_comments":       "",
	})

	return []llm.Message{
		llm.NewTextMessage("system", systemContent),
		llm.NewTextMessage("user", user),
	}
}

// renderDiffsForFile produces the <file>...</file> block for one diff, the
// shape the OCR system prompt expects. Renamed files gain a renamed_from
// attribute so the reviewer treats the change as a move and does not re-flag
// issues that already existed pre-rename. Pure renames (no line changes)
// collapse to a single note line in place of the (empty) diff body.
func renderDiffsForFile(d model.Diff) string {
	path := d.NewPath
	if path == "" || path == "/dev/null" {
		path = d.OldPath
	}
	var b strings.Builder
	b.WriteString("<file path=\"")
	b.WriteString(path)
	b.WriteString("\"")
	if d.IsRenamed && d.OldPath != "" && d.OldPath != path {
		b.WriteString(" renamed_from=\"")
		b.WriteString(d.OldPath)
		b.WriteString("\"")
	}
	b.WriteString(">\n")
	if isPureRename(d) {
		b.WriteString("Renamed from ")
		b.WriteString(d.OldPath)
		b.WriteString(" to ")
		b.WriteString(path)
		b.WriteString(" with no content change.\n")
	} else {
		b.WriteString(d.Diff)
		if !strings.HasSuffix(d.Diff, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString("</file>\n")
	return b.String()
}

// isPureRename reports whether this diff is a rename/move with no line
// changes on either side. Used to skip the LLM call for the file entirely —
// there is nothing to review.
func isPureRename(d model.Diff) bool {
	return d.IsRenamed && d.Insertions == 0 && d.Deletions == 0
}

// renderChangedFilesJSON lists the paths of every diff in the run for the
// {{change_files}} placeholder.
func renderChangedFilesJSON(diffs []model.Diff) string {
	paths := make([]string, 0, len(diffs))
	for _, d := range diffs {
		p := d.NewPath
		if p == "" || p == "/dev/null" {
			p = d.OldPath
		}
		paths = append(paths, p)
	}
	b, err := json.Marshal(paths)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// loadMainToolDefs parses the embedded tools.json and returns the LLM tool
// definitions to advertise on every MAIN_TASK request.
func loadMainToolDefs() ([]llm.ToolDef, error) {
	entries, err := tool.LoadToolsConfig("")
	if err != nil {
		return nil, err
	}
	defs := make([]llm.ToolDef, 0, len(entries))
	for _, e := range entries {
		raw, ok := e.ToolDefsByPhase(false)
		if !ok {
			continue
		}
		var fn llm.FunctionDef
		if err := json.Unmarshal(raw, &fn); err != nil {
			return nil, fmt.Errorf("tools.json: parse %s: %w", e.Name, err)
		}
		fn.RawDefinition = raw
		defs = append(defs, llm.ToolDef{Type: "function", Function: fn})
	}
	return defs, nil
}

// buildCompressionTemplate loads the memory-compression conversation template
// used by llmloop when the running conversation crosses its token budget.
func buildCompressionTemplate() (llmloop.LlmConversation, error) {
	sys, err := loadPrompt("memory_compression_task_system.md")
	if err != nil {
		return llmloop.LlmConversation{}, err
	}
	user, err := loadPrompt("memory_compression_task_user.md")
	if err != nil {
		return llmloop.LlmConversation{}, err
	}
	return llmloop.LlmConversation{
		Messages: []llmloop.ChatMessage{
			{Role: "system", Content: sys},
			{Role: "user", Content: user},
		},
	}, nil
}
