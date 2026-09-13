// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/indexer.py under Apache License 2.0.

package index

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/shubam-disseqt/z-code-reviewer/internal/filetype"
)

// Concurrency + size thresholds. Match Mira exactly — see PORTING.md §7
// for why _LLM_SEMAPHORE=8 and _BATCH_SIZE=3 aren't parameters.
const (
	llmSemaphore       = 8
	fileFetchSemaphore = 10
	batchSize          = 3
	largeFileBytes     = 5000
	trivialFileBytes   = 600
)

// skipPatterns are the globs the indexer always ignores. Matched with
// path.Match against both the full repo-relative path and the basename,
// mirroring Mira's fnmatch pass. Prefix globs like "node_modules/*" are
// checked against the head of the path.
var skipPatterns = []string{
	"*.lock",
	"*.lockb",
	"package-lock.json",
	"yarn.lock",
	"pnpm-lock.yaml",
	"Pipfile.lock",
	"poetry.lock",
	"go.sum",
	"*.min.js",
	"*.min.css",
	"*.map",
	"*.svg",
	"*.png",
	"*.jpg",
	"*.jpeg",
	"*.gif",
	"*.ico",
	"*.woff",
	"*.woff2",
	"*.ttf",
	"*.eot",
	"*.pdf",
	"*.zip",
	"*.tar.gz",
	"*.gz",
	"*.bz2",
	"*.exe",
	"*.dll",
	"*.so",
	"*.dylib",
	"node_modules/*",
	"vendor/*",
	".git/*",
	"__pycache__/*",
	"dist/*",
	"build/*",
	".next/*",
	".nuxt/*",
}

// shouldIndex reports whether repoPath should be summarized. extraExcludes
// are user-configured globs layered on top of the built-ins.
func shouldIndex(repoPath string, extraExcludes []string) bool {
	base := filepath.Base(repoPath)
	for _, pat := range skipPatterns {
		if matchGlob(pat, repoPath) || matchGlob(pat, base) {
			return false
		}
	}
	for _, pat := range extraExcludes {
		if matchGlob(pat, repoPath) || matchGlob(pat, base) {
			return false
		}
	}
	return filetype.IsIndexablePath(base)
}

// matchGlob handles Mira's fnmatch-style patterns: leading `**` is not used,
// and prefix patterns like "node_modules/*" match any path beginning with
// "node_modules/". path.Match's own trailing-`*` only matches within one
// segment, so we combine it with a prefix check.
func matchGlob(pattern, name string) bool {
	if ok, err := path.Match(pattern, name); err == nil && ok {
		return true
	}
	// Prefix-directory case: "foo/*" also means "foo/anything/anywhere".
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*") + "/"
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// contentHash returns the SHA-256 hex digest of s after replacing invalid
// UTF-8 with U+FFFD. Matches Python's `s.encode("utf-8", errors="replace")`
// so a corrupted-bytes file hashes stably across runs.
func contentHash(s string) string {
	sum := sha256.Sum256([]byte(replaceInvalidUTF8(s)))
	return hex.EncodeToString(sum[:])
}

// replaceInvalidUTF8 substitutes U+FFFD for any invalid rune in s. Fast
// path: if s is already valid UTF-8 (the common case), return it verbatim.
func replaceInvalidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
			i++
			continue
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

// countLOC returns the number of lines in content — Mira semantics: newline
// count plus 1 if the file doesn't end with a newline, or 0 for empty.
func countLOC(content string) int {
	if content == "" {
		return 0
	}
	n := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		n++
	}
	return n
}

// filePair is one (path, content) waiting to be summarized.
type filePair struct {
	Path    string
	Content string
}

// buildBatches groups file pairs for the summarize call. Files >=
// largeFileBytes get solo batches so one 8k-line file can't stretch a
// three-file batch to 80s+. Everything else packs into batches of
// batchSize preserving input order.
func buildBatches(pairs []filePair) [][]filePair {
	if len(pairs) == 0 {
		return nil
	}
	var large, small []filePair
	for _, p := range pairs {
		if len(p.Content) >= largeFileBytes {
			large = append(large, p)
		} else {
			small = append(small, p)
		}
	}
	batches := make([][]filePair, 0, len(large)+len(small)/batchSize+1)
	for _, p := range large {
		batches = append(batches, []filePair{p})
	}
	for i := 0; i < len(small); i += batchSize {
		end := i + batchSize
		if end > len(small) {
			end = len(small)
		}
		batches = append(batches, small[i:end])
	}
	return batches
}

// isTrivial reports whether a file is small enough to skip the LLM. Still
// gets a stored row (empty summary, language from extension, content hash)
// so blast-radius and path queries see it.
func isTrivial(content string) bool { return len(content) < trivialFileBytes }
