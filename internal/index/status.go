// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/index/status.py under Apache License 2.0.

package index

import (
	"sync"
	"time"
)

// Status is a small thread-safe progress tracker for a single indexing run.
// Cheaper than Mira's per-repo dict tracker — the reviewer indexes one repo
// per invocation and the caller owns the value.
type Status struct {
	mu        sync.Mutex
	total     int
	done      int
	startedAt time.Time
	endedAt   time.Time
	failed    bool
	err       string
}

// StatusSnapshot is a copy safe to publish outside the lock.
type StatusSnapshot struct {
	Total     int
	Done      int
	StartedAt time.Time
	EndedAt   time.Time
	Failed    bool
	Err       string
}

// Start marks the beginning of a run of total files.
func (s *Status) Start(total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total = total
	s.done = 0
	s.startedAt = time.Now()
	s.endedAt = time.Time{}
	s.failed = false
	s.err = ""
}

// Increment adds n to the completed count.
func (s *Status) Increment(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done += n
}

// Finish marks the run as complete. err is stored if non-empty.
func (s *Status) Finish(err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endedAt = time.Now()
	if err != "" {
		s.failed = true
		s.err = err
	}
}

// Snapshot returns the current progress as a copy.
func (s *Status) Snapshot() StatusSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return StatusSnapshot{
		Total:     s.total,
		Done:      s.done,
		StartedAt: s.startedAt,
		EndedAt:   s.endedAt,
		Failed:    s.failed,
		Err:       s.err,
	}
}
