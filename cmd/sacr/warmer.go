// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/index"
	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/logutil"
)

// warmerLogDir names the directory that holds the warmer's stdout/stderr
// logs. Sibling to the session directory so operators find both in the same
// place. Overridable via SACR_SESSION_DIR (same root as sessions).
func warmerLogDir() (string, error) {
	if dir := os.Getenv("SACR_SESSION_DIR"); dir != "" {
		return filepath.Dir(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".sacr"), nil
}

// missingSummaryPaths returns the subset of `changed` for which the store has
// no summary row. Store errors are surfaced so the caller can decide whether
// to warn or bail — this function itself never spawns anything.
func missingSummaryPaths(ctx context.Context, store index.Store, changed []string) ([]string, error) {
	if store == nil || len(changed) == 0 {
		return nil, nil
	}
	rows, err := store.ListSummaries(ctx, changed)
	if err != nil {
		return nil, fmt.Errorf("list summaries: %w", err)
	}
	have := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		have[r.Path] = struct{}{}
	}
	var missing []string
	for _, p := range changed {
		if _, ok := have[p]; !ok {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

// spawnIndexWarmer launches `sacr index --paths <csv>` as a detached
// subprocess and returns immediately. The child inherits the parent's env
// (including SACR_DB_URL + provider creds) but writes its own stdio to a
// log file so the review's output stays clean. Best-effort — a spawn failure
// is logged but never fatal for the review.
//
// The child continues after this process exits: Go's exec.Cmd doesn't Wait
// unless we call Wait, and the child's stdio is redirected off any terminal.
func spawnIndexWarmer(repo string, paths []string, logger *slog.Logger) {
	if len(paths) == 0 {
		return
	}
	log := logutil.WithStage(logger, "warmer")
	self, err := os.Executable()
	if err != nil {
		log.Warn("resolve self, skipping", "err", err.Error())
		return
	}
	dir, err := warmerLogDir()
	if err != nil {
		log.Warn("resolve log dir, skipping", "err", err.Error())
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Warn("mkdir failed, skipping", "dir", dir, "err", err.Error())
		return
	}
	logPath := filepath.Join(dir, "warmer.log")
	// Append so successive warms build one log. Line count grows slowly.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Warn("open log failed, skipping", "path", logPath, "err", err.Error())
		return
	}
	// Do NOT defer f.Close() — the child needs the fd. The kernel closes it
	// when the child exits.

	cmd := exec.Command(self, "index", "--repo", repo, "--paths", strings.Join(paths, ","))
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.Stdin = nil
	// Env inherits by default when Env is nil.

	if err := cmd.Start(); err != nil {
		log.Warn("start failed, skipping", "err", err.Error())
		_ = f.Close()
		return
	}
	// Release the child — we won't Wait for it. The Go runtime doesn't zombie
	// so long as the OS reaps orphaned children (init on Unix).
	_ = cmd.Process.Release()

	// Legacy-compat: text-mode line reads "warmer: N file(s) missing from
	// index — spawned sacr index (pid P, log LOG)" so operators grep the
	// same way as pre-slog. JSON mode surfaces the same as structured attrs.
	log.Info(
		fmt.Sprintf("%d file(s) missing from index — spawned sacr index", len(paths)),
		"count", len(paths),
		"pid", cmd.Process.Pid,
		"log", logPath,
	)
}
