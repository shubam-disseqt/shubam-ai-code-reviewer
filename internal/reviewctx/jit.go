// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/jit_context.py under
// Apache License 2.0.

package reviewctx

import (
	"context"
	"path"
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/filetype"
)

// BuildJIT is the index-less path. For each changed file with source
// available, it extracts imports, resolves them to repo-relative paths,
// filters against RepoTree, and inlines the imported file's source
// (excerpt-capped). Cap on both files fetched and total chars emitted.
//
// Returns "" (no error) when there's nothing useful to say.
func BuildJIT(_ context.Context, opts Options) (string, error) {
	o := opts.resolved()
	if len(o.ChangedPaths) == 0 || len(o.NewFileContent) == 0 || o.CharBudget <= 0 {
		return "", nil
	}

	repoTree := makeSet(o.RepoTree)
	changed := makeSet(o.ChangedPaths)

	// One-shot go.mod parse makes in-repo Go resolution deterministic.
	goModule := ""
	if o.EnableJavaGo && hasGoFile(o.ChangedPaths) {
		if repoTree == nil || containsGoMod(repoTree) {
			if src, ok := o.NewFileContent["go.mod"]; ok {
				goModule = ParseGoModule(src)
			}
		}
	}

	var parts []string
	charsUsed := 0
	filesAdded := 0
	seenImports := make(map[string]struct{})

	for _, changedPath := range o.ChangedPaths {
		if filesAdded >= o.MaxFiles || charsUsed >= o.CharBudget {
			break
		}
		lang := filetype.LanguageFromPath(changedPath)
		if lang == filetype.LangUnknown {
			continue
		}
		source, ok := o.NewFileContent[changedPath]
		if !ok || source == "" {
			continue
		}

		candidates := ExtractImportCandidates(
			source, lang, changedPath, repoTree, o.EnableJavaGo, goModule,
		)

		for _, cand := range candidates {
			if filesAdded >= o.MaxFiles || charsUsed >= o.CharBudget {
				break
			}
			if _, dup := seenImports[cand]; dup {
				continue
			}
			if _, isChanged := changed[cand]; isChanged {
				continue
			}
			if repoTree != nil {
				if _, exists := repoTree[cand]; !exists {
					continue
				}
			}
			seenImports[cand] = struct{}{}

			importedSrc, ok := o.NewFileContent[cand]
			if !ok || importedSrc == "" {
				// JIT contract: NewFileContent may not have the imported file.
				// Skip silently — the fetch layer belongs upstream.
				continue
			}

			candLang := filetype.LanguageFromPath(cand)
			if candLang == filetype.LangUnknown {
				candLang = lang
			}
			excerpt := trimExcerpt(importedSrc, o.MaxPerFileChars)
			if strings.TrimSpace(excerpt) == "" {
				continue
			}

			block := formatJITBlock(cand, changedPath, candLang, excerpt)
			if charsUsed+len(block) > o.CharBudget {
				break
			}
			parts = append(parts, block)
			charsUsed += len(block)
			filesAdded++
		}
	}

	if len(parts) == 0 {
		return "", nil
	}
	header := "## Related file context\n\n" +
		"Source excerpts from files imported by the changed files. Use these to " +
		"verify call signatures and downstream behaviour instead of speculating.\n\n"
	return header + strings.Join(parts, "\n\n"), nil
}

// formatJITBlock produces one file's markdown fence.
func formatJITBlock(candPath, importer string, lang filetype.Language, excerpt string) string {
	var b strings.Builder
	b.WriteString("### `")
	b.WriteString(candPath)
	b.WriteString("` (imported by `")
	b.WriteString(importer)
	b.WriteString("`)\n")
	b.WriteString("```")
	if lang != filetype.LangUnknown {
		b.WriteString(string(lang))
	}
	b.WriteString("\n")
	b.WriteString(excerpt)
	if !strings.HasSuffix(excerpt, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("```")
	return b.String()
}

// trimExcerpt trims source to <= maxChars, ending on a line boundary when
// possible. Mirrors Mira _trim_symbol.
func trimExcerpt(source string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	if len(source) <= maxChars {
		return source
	}
	truncated := source[:maxChars]
	if idx := strings.LastIndex(truncated, "\n"); idx > 0 {
		truncated = truncated[:idx]
	}
	return truncated + "\n// ... (truncated)"
}

// makeSet builds a lookup set from a slice, or returns nil for empty input.
func makeSet(xs []string) map[string]struct{} {
	if len(xs) == 0 {
		return nil
	}
	s := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		s[x] = struct{}{}
	}
	return s
}

func hasGoFile(paths []string) bool {
	for _, p := range paths {
		if path.Ext(p) == ".go" {
			return true
		}
	}
	return false
}

func containsGoMod(tree map[string]struct{}) bool {
	_, ok := tree["go.mod"]
	return ok
}
