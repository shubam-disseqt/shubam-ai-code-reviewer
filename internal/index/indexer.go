// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/indexer.py under Apache License 2.0.

package index

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/shubam-disseqt/z-code-reviewer/internal/filetype"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"

	"golang.org/x/sync/errgroup"
)

// IndexerOptions tunes an indexing run. All fields are optional.
type IndexerOptions struct {
	// Model is the LLM model identifier passed to CompletionsWithCtx. Empty
	// lets the client's own default apply.
	Model string
	// ModelMaxOutputTokens caps the summarize call's max_tokens field.
	// 0 falls back to the internal default (16384, hard ceiling 32768).
	ModelMaxOutputTokens int
	// ExtraExcludes are user-provided globs layered on top of the built-in
	// skip list.
	ExtraExcludes []string
	// MaxFileBytes drops any file whose UTF-8 byte length exceeds this
	// threshold. 0 means "no cap".
	MaxFileBytes int
	// Full disables the content-hash guard: every file is re-summarized.
	Full bool
	// Logger receives structured progress/error events. Nil defaults to
	// slog.Default().
	Logger *slog.Logger
	// Status is an optional progress publisher.
	Status *Status
}

// Indexer summarizes source files and persists their FileSummary rows.
type Indexer struct {
	store  Store
	client llm.LLMClient
	opts   IndexerOptions
	logger *slog.Logger
}

// NewIndexer constructs an Indexer. store owns persistence, client makes
// summarize calls, opts tunes behavior.
func NewIndexer(store Store, client llm.LLMClient, opts IndexerOptions) *Indexer {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Indexer{store: store, client: client, opts: opts, logger: logger}
}

// IndexRepo indexes every eligible file under repoRoot. Returns the count of
// files persisted (trivial files included). Cleans up index rows for files
// that no longer exist. Also runs manifest cleanup (empty live set is still
// meaningful — see PORTING.md §7).
func (i *Indexer) IndexRepo(ctx context.Context, repoRoot string) (int, error) {
	if repoRoot == "" {
		return 0, fmt.Errorf("index: repoRoot is required")
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return 0, fmt.Errorf("index: resolve repoRoot: %w", err)
	}

	tree, err := walkRepoTree(absRoot)
	if err != nil {
		return 0, err
	}

	indexable := make([]string, 0, len(tree))
	for _, p := range tree {
		if shouldIndex(p, i.opts.ExtraExcludes) {
			indexable = append(indexable, p)
		}
	}

	// Delete rows for paths that vanished from the working tree — runs
	// once per full run before we start writing new summaries.
	if existing, err := i.store.ListAllPaths(ctx); err == nil {
		treeSet := make(map[string]struct{}, len(tree))
		for _, p := range tree {
			treeSet[p] = struct{}{}
		}
		var deleted []string
		for _, p := range existing {
			if _, ok := treeSet[p]; !ok {
				deleted = append(deleted, p)
			}
		}
		if len(deleted) > 0 {
			if err := i.store.RemovePaths(ctx, deleted); err != nil {
				i.logger.Warn("index: remove deleted paths", "err", err, "count", len(deleted))
			}
		}
	}

	indexed, err := i.indexPaths(ctx, absRoot, indexable)
	if err != nil {
		return indexed, err
	}

	// Manifest pass — pure parsers, no LLM. Failures are logged, not fatal:
	// a broken package.json shouldn't nuke a completed file-summarize run.
	if err := IndexManifests(ctx, i.store, absRoot, tree, i.logger); err != nil {
		i.logger.Warn("index: manifest pass", "err", err)
	}

	// Directory summarization — batched LLM call. Skipped when no file was
	// (re)persisted this run, so a no-change re-index issues zero LLM calls
	// (matches the content-hash guard's contract).
	if i.client != nil && indexed > 0 {
		if err := SummarizeDirectories(ctx, i.store, i.client, i.opts.Model, i.opts.ModelMaxOutputTokens, i.logger); err != nil {
			i.logger.Warn("index: dir summarization pass", "err", err)
		}
	}

	return indexed, nil
}

// IndexDiff re-summarizes exactly the changed paths. Deleted paths are the
// caller's responsibility — pass those through RemovePaths separately.
func (i *Indexer) IndexDiff(ctx context.Context, repoRoot string, changedPaths []string) (int, error) {
	if repoRoot == "" {
		return 0, fmt.Errorf("index: repoRoot is required")
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return 0, fmt.Errorf("index: resolve repoRoot: %w", err)
	}
	var toIndex []string
	for _, p := range changedPaths {
		if shouldIndex(p, i.opts.ExtraExcludes) {
			toIndex = append(toIndex, p)
		}
	}
	return i.indexPaths(ctx, absRoot, toIndex)
}

// indexPaths reads every candidate, splits into trivial vs summarizable,
// fires all batches through one errgroup (see PORTING.md §7: fire-all-at-once
// avoids the LLM-semaphore serial-await bug), and writes each result
// autocommit. Returns the number of files persisted.
func (i *Indexer) indexPaths(ctx context.Context, absRoot string, indexable []string) (int, error) {
	if i.opts.Status != nil {
		i.opts.Status.Start(len(indexable))
	}

	trivial, summarizable, err := i.readAndSplit(ctx, absRoot, indexable)
	if err != nil {
		return 0, err
	}

	indexed := 0

	// Persist trivial files first — deterministic and independent of the LLM.
	for _, p := range trivial {
		fs := FileSummary{
			Path:        p.Path,
			Language:    filetype.LanguageFromPath(p.Path),
			Summary:     "",
			ContentHash: contentHash(p.Content),
			LOC:         countLOC(p.Content),
		}
		if err := i.store.UpsertSummary(ctx, fs); err != nil {
			i.logger.Warn("index: upsert trivial", "path", p.Path, "err", err)
			continue
		}
		indexed++
		if i.opts.Status != nil {
			i.opts.Status.Increment(1)
		}
	}

	if len(summarizable) == 0 {
		if i.opts.Status != nil {
			i.opts.Status.Finish("")
		}
		return indexed, nil
	}

	batches := buildBatches(summarizable)

	// One errgroup, all batches. Inner SetLimit(llmSemaphore) caps parallel
	// summarize calls. Errors inside a batch are swallowed — one bad batch
	// doesn't nuke the whole run. Only ctx.Canceled propagates.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(llmSemaphore)

	// results channel is sized so producer goroutines never block on a slow
	// writer. Store writes serialize on the store's own mu.
	type batchResult struct {
		path    string
		content string
		data    summarizeFileJSON
	}
	resultCh := make(chan batchResult, len(summarizable))

	for _, batch := range batches {
		batch := batch
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			parsed, err := summarizeBatch(gctx, i.client, i.opts.Model, i.opts.ModelMaxOutputTokens, batch)
			if err != nil {
				// Skip bad batches — logged, not fatal. Context cancellation is
				// the one exception.
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				i.logger.Warn("index: skip failed batch", "size", len(batch), "err", err)
				return nil
			}
			byPath := make(map[string]summarizeFileJSON, len(parsed))
			for _, f := range parsed {
				if f.Path != "" {
					byPath[f.Path] = f
				}
			}
			for _, fp := range batch {
				data, ok := byPath[fp.Path]
				if !ok {
					// LLM dropped this file from its response — nothing to
					// persist, quietly skip.
					continue
				}
				resultCh <- batchResult{path: fp.Path, content: fp.Content, data: data}
			}
			return nil
		})
	}

	// Drain results in a helper goroutine so writes proceed while batches
	// are still running. Closing resultCh after Wait() lets the drain loop
	// exit cleanly.
	go func() {
		_ = g.Wait()
		close(resultCh)
	}()

	for r := range resultCh {
		summary := buildFileSummary(r.path, r.content, r.data)
		if err := i.store.UpsertSummary(ctx, summary); err != nil {
			i.logger.Warn("index: upsert summary", "path", r.path, "err", err)
			continue
		}
		indexed++
		if i.opts.Status != nil {
			i.opts.Status.Increment(1)
		}
	}

	// Surface a ctx-cancelled error if one occurred; other errors were
	// already logged and swallowed above.
	if err := ctx.Err(); err != nil {
		if i.opts.Status != nil {
			i.opts.Status.Finish(err.Error())
		}
		return indexed, err
	}
	if i.opts.Status != nil {
		i.opts.Status.Finish("")
	}
	return indexed, nil
}

// readAndSplit reads each indexable file, applies the size cap and content-
// hash guard, and returns two buckets: trivial (get placeholder rows) and
// summarizable (get LLM calls).
func (i *Indexer) readAndSplit(ctx context.Context, absRoot string, paths []string) (trivial, summarizable []filePair, err error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}
	// Bounded concurrency mirrors _FILE_FETCH_SEMAPHORE. Local FS reads are
	// cheap, but the cap keeps memory bounded on big repos.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fileFetchSemaphore)

	type fileRead struct {
		path    string
		content string
		skip    bool
	}
	results := make([]fileRead, len(paths))

	for idx, p := range paths {
		idx, p := idx, p
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			full := filepath.Join(absRoot, p)
			data, readErr := os.ReadFile(full)
			if readErr != nil {
				i.logger.Debug("index: read file", "path", p, "err", readErr)
				results[idx] = fileRead{path: p, skip: true}
				return nil
			}
			if i.opts.MaxFileBytes > 0 && len(data) > i.opts.MaxFileBytes {
				i.logger.Debug("index: file over size cap", "path", p, "bytes", len(data))
				results[idx] = fileRead{path: p, skip: true}
				return nil
			}
			results[idx] = fileRead{path: p, content: string(data)}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	for _, r := range results {
		if r.skip {
			continue
		}
		// Content-hash guard — skip unchanged files unless --full.
		if !i.opts.Full {
			existing, err := i.store.GetSummary(ctx, r.path)
			if err == nil && existing.ContentHash == contentHash(r.content) {
				continue
			}
		}
		if isTrivial(r.content) {
			trivial = append(trivial, filePair{Path: r.path, Content: r.content})
		} else {
			summarizable = append(summarizable, filePair{Path: r.path, Content: r.content})
		}
	}
	// Stable ordering so tests are deterministic.
	sort.Slice(trivial, func(a, b int) bool { return trivial[a].Path < trivial[b].Path })
	sort.Slice(summarizable, func(a, b int) bool { return summarizable[a].Path < summarizable[b].Path })
	return trivial, summarizable, nil
}

// walkRepoTree returns every regular file under root, expressed as a repo-
// relative slash-path. Symlinks are skipped. Errors on individual entries
// are logged and dropped so an unreadable file can't abort a whole repo
// walk.
func walkRepoTree(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort — skip unreadable entries
		}
		if d.IsDir() {
			// Prune obvious skip dirs early so we don't descend into
			// node_modules / .git. Cheaper than matching every child later.
			base := d.Name()
			if base == ".git" || base == "node_modules" || base == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("index: walk repo tree: %w", err)
	}
	sort.Strings(out)
	return out, nil
}
