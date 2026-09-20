// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package gh

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAddLabels(t *testing.T) {
	var got []string
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got, want := r.URL.Path, "/api/v3/repos/o/r/issues/7/labels"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		// go-github sends a raw JSON array of label names for this endpoint.
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
		mustJSON(w, []map[string]any{{"name": "bug"}, {"name": "security"}})
	})
	if err := c.AddLabels(context.Background(), "o", "r", 7, []string{"bug", "security"}); err != nil {
		t.Fatalf("AddLabels: %v", err)
	}
	if len(got) != 2 || got[0] != "bug" || got[1] != "security" {
		t.Errorf("labels payload = %v", got)
	}
}

func TestAddLabels_EmptyNoCall(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP call for empty labels")
	})
	if err := c.AddLabels(context.Background(), "o", "r", 7, nil); err != nil {
		t.Errorf("AddLabels(nil): %v", err)
	}
	if err := c.AddLabels(context.Background(), "o", "r", 7, []string{}); err != nil {
		t.Errorf("AddLabels([]): %v", err)
	}
}

func TestAddLabels_WrapsError(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"nope"}`))
	})
	err := c.AddLabels(context.Background(), "o", "r", 7, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "gh:") {
		t.Errorf("err = %v, want wrapped", err)
	}
}

func TestUploadSARIF_EncodesGzipBase64(t *testing.T) {
	origin := []byte(`{"version":"2.1.0","runs":[]}`)
	var payload struct {
		CommitSHA string `json:"commit_sha"`
		Ref       string `json:"ref"`
		Sarif     string `json:"sarif"`
		ToolName  string `json:"tool_name"`
	}
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got, want := r.URL.Path, "/api/v3/repos/o/r/code-scanning/sarifs"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"abc123","url":"https://example.test/x"}`))
	})
	id, err := c.UploadSARIF(context.Background(), "o", "r", "deadbeef", "refs/heads/main", origin)
	if err != nil {
		t.Fatalf("UploadSARIF: %v", err)
	}
	if id != "abc123" {
		t.Errorf("id = %q, want abc123", id)
	}
	if payload.CommitSHA != "deadbeef" || payload.Ref != "refs/heads/main" || payload.ToolName != "sacr" {
		t.Errorf("payload = %+v", payload)
	}
	// Round-trip decode: base64 → gzip → original bytes.
	dec, err := base64.StdEncoding.DecodeString(payload.Sarif)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(dec))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	unpacked, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	if !bytes.Equal(unpacked, origin) {
		t.Errorf("round-trip mismatch: got %q, want %q", unpacked, origin)
	}
}

func TestUploadSARIF_WrapsError(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"schema"}`))
	})
	_, err := c.UploadSARIF(context.Background(), "o", "r", "sha", "refs/heads/main", []byte("{}"))
	if err == nil || !strings.Contains(err.Error(), "gh:") {
		t.Errorf("err = %v, want wrapped", err)
	}
}

func TestWaitSARIF_TerminatesOnComplete(t *testing.T) {
	var calls int32
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v3/repos/o/r/code-scanning/sarifs/id-1"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		n := atomic.AddInt32(&calls, 1)
		status := "pending"
		if n >= 2 {
			status = "complete"
		}
		mustJSON(w, map[string]any{"processing_status": status})
	})
	// Tight ctx — we don't want the 2s poll delay dominating test time.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := c.WaitSARIF(ctx, "o", "r", "id-1")
	if err != nil {
		t.Fatalf("WaitSARIF: %v", err)
	}
	if status != "complete" {
		t.Errorf("status = %q, want complete", status)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("calls = %d, want ≥ 2 (polled)", calls)
	}
}

func TestWaitSARIF_ReturnsFailedTerminal(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		mustJSON(w, map[string]any{"processing_status": "failed"})
	})
	status, err := c.WaitSARIF(context.Background(), "o", "r", "id-2")
	if err != nil {
		t.Fatalf("WaitSARIF: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed", status)
	}
}

func TestWaitSARIF_EmptyID(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP call for empty id")
	})
	_, err := c.WaitSARIF(context.Background(), "o", "r", "")
	if err == nil {
		t.Fatal("want error for empty id")
	}
}

func TestWaitSARIF_ContextCanceled(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		mustJSON(w, map[string]any{"processing_status": "pending"})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.WaitSARIF(ctx, "o", "r", "id-3")
	if err == nil {
		t.Fatal("want ctx error")
	}
}
