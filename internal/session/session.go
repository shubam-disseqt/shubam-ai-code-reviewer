// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt
//
// Adapted from alibaba/open-code-review internal/session/history.go and
// persist.go under Apache License 2.0. Modifications: dropped viewer,
// comparison, list, raw_writer, manifest coverage, sealed-input identity, and
// orphan-request resume; simplified to the interface declared by
// internal/llmloop.Session.

// Package session provides a persistent, append-only JSONL log of an
// llmloop run. One Session per run; one FileSession per taskKey; one
// TaskRecord per LLM call within that task. Writes are serialized by an
// internal mutex — llmloop calls into the session at request granularity, so
// there's no lock contention worth optimizing away.
package session

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shubam-disseqt/z-code-reviewer/internal/llmloop"
)

// Compile-time check: Session satisfies llmloop.Session. If llmloop's
// interface drifts this will refuse to build, which is the whole point.
var _ llmloop.Session = (*Session)(nil)

// Session is the run-scoped append log. Concurrent-safe via mu.
type Session struct {
	mu           sync.Mutex
	sessionID    string
	startTime    time.Time
	file         *os.File
	writer       *bufio.Writer
	lastUUID     string
	closed       bool
	fileSessions sync.Map // taskKey -> *FileSession (identity-preserving)
	llmFailures  int64
}

// New opens (creates if missing) `dir/<sessionID>.jsonl` and writes the
// session_start record. Empty sessionID gets a fresh UUIDv4.
func New(dir, sessionID string) (*Session, error) {
	if sessionID == "" {
		sessionID = generateUUID()
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open session file: %w", err)
	}
	s := &Session{
		sessionID: sessionID,
		startTime: time.Now(),
		file:      f,
		writer:    bufio.NewWriter(f),
	}
	s.writeSessionStart()
	return s, nil
}

// SessionID returns the run identifier used for cache affinity.
func (s *Session) SessionID() string { return s.sessionID }

// GetOrCreateFileSession returns the FileSession for taskKey, creating one on
// first access. Identity-preserving: repeat calls with the same key return
// the same instance so records stay chained under one parent.
func (s *Session) GetOrCreateFileSession(taskKey string) llmloop.FileSession {
	if fs, ok := s.fileSessions.Load(taskKey); ok {
		return fs.(*FileSession)
	}
	fs := &FileSession{session: s, taskKey: taskKey, taskRecords: make(map[llmloop.TaskType]int)}
	actual, _ := s.fileSessions.LoadOrStore(taskKey, fs)
	return actual.(*FileSession)
}

// Close flushes the writer, writes session_end, and closes the file. Safe to
// call more than once; subsequent calls are no-ops.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	filesReviewed := 0
	s.fileSessions.Range(func(_, _ any) bool {
		filesReviewed++
		return true
	})

	uuid := generateUUID()
	rec := sessionEndRecord{
		baseRecord: baseRecord{
			UUID:       uuid,
			ParentUUID: s.lastUUID,
			Type:       typeSessionEnd,
			SessionID:  s.sessionID,
			Timestamp:  time.Now().UTC(),
		},
		FilesReviewed:   filesReviewed,
		DurationSeconds: time.Since(s.startTime).Seconds(),
		LLMFailures:     int(atomic.LoadInt64(&s.llmFailures)),
	}
	s.writeRecordLocked(rec)
	s.lastUUID = uuid

	var firstErr error
	if err := s.writer.Flush(); err != nil {
		firstErr = fmt.Errorf("flush session file: %w", err)
	}
	if err := s.file.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("close session file: %w", err)
	}
	return firstErr
}

// writeSessionStart runs once during New, before the Session escapes to any
// caller. No lock needed.
func (s *Session) writeSessionStart() {
	uuid := generateUUID()
	rec := sessionStartRecord{
		baseRecord: baseRecord{
			UUID:      uuid,
			Type:      typeSessionStart,
			SessionID: s.sessionID,
			Timestamp: s.startTime.UTC(),
		},
	}
	s.writeRecordLocked(rec)
	s.lastUUID = uuid
}

// writeRecordLocked marshals and appends one record. Caller must hold s.mu,
// except for writeSessionStart which runs before the Session escapes.
// Errors are surfaced to stderr rather than returned — the JSONL is a log,
// not a transactional store, and the running loop is not the place to fail
// on a corrupted disk when the actual work is still salvageable.
func (s *Session) writeRecordLocked(rec any) {
	data, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session: marshal record: %v\n", err)
		return
	}
	if _, err := s.writer.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "session: write record: %v\n", err)
		return
	}
	if err := s.writer.WriteByte('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "session: write newline: %v\n", err)
	}
}

// newBase mints a UUID under the lock, chains it off the previous record's
// UUID, and returns the ready-to-embed header. Caller holds s.mu.
func (s *Session) newBaseLocked(recordType string) baseRecord {
	uuid := generateUUID()
	return baseRecord{
		UUID:       uuid,
		ParentUUID: s.lastUUID,
		Type:       recordType,
		SessionID:  s.sessionID,
		Timestamp:  time.Now().UTC(),
	}
}

// commitLocked writes and flushes one already-stamped record, then advances
// lastUUID. Caller holds s.mu.
func (s *Session) commitLocked(uuid string, rec any) {
	s.writeRecordLocked(rec)
	s.lastUUID = uuid
	// Flush per record: cheap on bufio, and a killed process leaves at most
	// one truncated line rather than a whole batch.
	if err := s.writer.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "session: flush: %v\n", err)
	}
}

// generateUUID returns a random UUIDv4 string, or a time-based fallback if
// crypto/rand fails (only happens on a broken kernel entropy source).
func generateUUID() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
