// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/indexer.py
// (_summarize_directories) under Apache License 2.0.

package index

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"sync"
	"text/template"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
	"github.com/shubam-disseqt/z-code-reviewer/internal/prompts"
	"golang.org/x/sync/errgroup"
)

const (
	// dirsPerBatch matches Mira's chunk size — one LLM call summarizes up to
	// 15 directories at once, amortizing the ~500ms per-request latency.
	dirsPerBatch = 15
	// filesPerDirCap caps how many per-file summary lines are shown to the
	// LLM for one directory, so a large dir does not blow the prompt budget.
	filesPerDirCap = 30
	// dirSummaryMaxTokens is the completion cap for one dir batch.
	dirSummaryMaxTokens = 4096
)

var (
	dirsTemplateOnce sync.Once
	dirsTemplateVal  *template.Template
	dirsTemplateErr  error
)

func getDirsTemplate() (*template.Template, error) {
	dirsTemplateOnce.Do(func() {
		data, err := prompts.Templates.ReadFile("summarize_dirs.md")
		if err != nil {
			dirsTemplateErr = fmt.Errorf("index: read summarize_dirs template: %w", err)
			return
		}
		tpl, err := template.New("summarize_dirs").Parse(string(data))
		if err != nil {
			dirsTemplateErr = fmt.Errorf("index: parse summarize_dirs template: %w", err)
			return
		}
		dirsTemplateVal = tpl
	})
	return dirsTemplateVal, dirsTemplateErr
}

// dirEntry is one enriched directory: the dir's path plus the per-file
// summary lines the LLM will condense into a one/two-sentence blurb.
type dirEntry struct {
	Path      string
	FileCount int
	Lines     []string
}

// dirTplEntry is the shape rendered into summarize_dirs.md.
type dirTplEntry struct {
	Display   string // "(root)" for empty path
	FileCount int
	Lines     []string
}

// dirSummaryResponse is the LLM's JSON shape.
type dirSummaryResponse struct {
	Directories []struct {
		Path    string `json:"path"`
		Summary string `json:"summary"`
	} `json:"directories"`
}

// SummarizeDirectories groups every indexed path by its parent directory and
// asks the LLM to summarize each dir in one/two sentences. Trivial-only
// directories (no file has an LLM summary) are skipped — nothing to condense.
// Directories are batched dirsPerBatch at a time; one bad batch is logged and
// skipped rather than aborting the run (see PORTING.md §7 semantics).
func SummarizeDirectories(ctx context.Context, store Store, client llm.LLMClient, model string, modelCap int, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	paths, err := store.ListAllPaths(ctx)
	if err != nil {
		return fmt.Errorf("index: list paths for dir summarization: %w", err)
	}
	if len(paths) == 0 {
		return nil
	}

	// Group by parent dir. Repo-root is the empty string, matching Mira.
	byDir := map[string][]string{}
	for _, p := range paths {
		parent := path.Dir(p)
		if parent == "." {
			parent = ""
		}
		byDir[parent] = append(byDir[parent], p)
	}

	// Build enriched entries in a deterministic order (sorted by dir path).
	dirPaths := make([]string, 0, len(byDir))
	for d := range byDir {
		dirPaths = append(dirPaths, d)
	}
	sort.Strings(dirPaths)

	enriched := make([]dirEntry, 0, len(dirPaths))
	for _, d := range dirPaths {
		files := byDir[d]
		// Cap the files-per-dir input to keep the prompt bounded on huge dirs.
		if len(files) > filesPerDirCap {
			files = files[:filesPerDirCap]
		}
		var lines []string
		for _, fp := range files {
			s, err := store.GetSummary(ctx, fp)
			if err != nil || s.Summary == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", path.Base(fp), s.Summary))
		}
		if len(lines) == 0 {
			continue
		}
		enriched = append(enriched, dirEntry{Path: d, FileCount: len(byDir[d]), Lines: lines})
	}
	if len(enriched) == 0 {
		return nil
	}

	// Chunk into fixed-size LLM batches.
	batches := chunkDirs(enriched, dirsPerBatch)

	// Concurrency: reuse the same LLM semaphore semantics as file summarize
	// so the two phases don't oversubscribe when they run back-to-back.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(llmSemaphore)

	var mu sync.Mutex
	writes := make([]DirectorySummary, 0)

	for _, batch := range batches {
		batch := batch
		g.Go(func() error {
			out, err := summarizeDirsBatch(gctx, client, model, modelCap, batch)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				logger.Warn("index: skip failed dir batch", "size", len(batch), "err", err)
				return nil
			}
			mu.Lock()
			writes = append(writes, out...)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	// gctx is cancelled once Wait returns; use the outer ctx for writes.
	for _, d := range writes {
		if err := store.UpsertDirectorySummary(ctx, d); err != nil {
			logger.Warn("index: upsert dir summary", "path", d.Path, "err", err)
		}
	}
	return nil
}

func chunkDirs(entries []dirEntry, size int) [][]dirEntry {
	if size < 1 {
		size = 1
	}
	out := make([][]dirEntry, 0, (len(entries)+size-1)/size)
	for i := 0; i < len(entries); i += size {
		end := i + size
		if end > len(entries) {
			end = len(entries)
		}
		out = append(out, entries[i:end])
	}
	return out
}

// summarizeDirsBatch renders the batch into the template, calls the LLM, and
// converts the response into DirectorySummary rows. Returns rows only for
// entries the LLM actually answered — silent-omitted dirs are dropped.
func summarizeDirsBatch(ctx context.Context, client llm.LLMClient, model string, modelCap int, batch []dirEntry) ([]DirectorySummary, error) {
	prompt, err := renderDirsPrompt(batch)
	if err != nil {
		return nil, err
	}
	temp := 0.0
	maxTokens := dirSummaryMaxTokens
	if modelCap > 0 && modelCap < maxTokens {
		maxTokens = modelCap
	}
	resp, err := client.CompletionsWithCtx(ctx, llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			llm.NewTextMessage("system", prompt),
			llm.NewTextMessage("user", "Summarize these directories."),
		},
		Temperature: &temp,
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("index: dir summarize batch: %w", err)
	}
	text := stripThinkBlocks(stripCodeFences(resp.Content()))
	var parsed dirSummaryResponse
	if err := json.NewDecoder(bytes.NewReader([]byte(text))).Decode(&parsed); err != nil {
		// One-shot repair pass — same lone-backslash fixup as file summarize.
		repaired := escapeLoneBackslashes(text)
		if err2 := json.NewDecoder(bytes.NewReader([]byte(repaired))).Decode(&parsed); err2 != nil {
			return nil, fmt.Errorf("index: parse dir summary response: %w", err)
		}
	}
	// Map returned entries back to the original batch by path.
	byPath := make(map[string]string, len(parsed.Directories))
	for _, d := range parsed.Directories {
		p := d.Path
		if p == "(root)" {
			p = ""
		}
		if d.Summary != "" {
			byPath[p] = d.Summary
		}
	}
	out := make([]DirectorySummary, 0, len(batch))
	for _, entry := range batch {
		s, ok := byPath[entry.Path]
		if !ok {
			continue
		}
		out = append(out, DirectorySummary{
			Path:      entry.Path,
			Summary:   s,
			FileCount: entry.FileCount,
		})
	}
	return out, nil
}

func renderDirsPrompt(batch []dirEntry) (string, error) {
	tpl, err := getDirsTemplate()
	if err != nil {
		return "", err
	}
	data := struct{ Dirs []dirTplEntry }{Dirs: make([]dirTplEntry, len(batch))}
	for i, e := range batch {
		display := e.Path
		if display == "" {
			display = "(root)"
		}
		data.Dirs[i] = dirTplEntry{Display: display, FileCount: e.FileCount, Lines: e.Lines}
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("index: render dir prompt: %w", err)
	}
	return buf.String(), nil
}
