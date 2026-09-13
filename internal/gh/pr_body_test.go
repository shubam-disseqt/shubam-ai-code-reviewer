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
	"testing"
)

func TestGetPRBody(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v3/repos/o/r/pulls/7"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		mustJSON(w, map[string]any{"number": 7, "body": "hello world"})
	})
	body, err := c.GetPRBody(context.Background(), "o", "r", 7)
	if err != nil {
		t.Fatalf("GetPRBody: %v", err)
	}
	if body != "hello world" {
		t.Errorf("body = %q, want hello world", body)
	}
}

func TestGetPRBody_WrapsError(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	})
	_, err := c.GetPRBody(context.Background(), "o", "r", 7)
	if err == nil || !strings.Contains(err.Error(), "gh:") {
		t.Errorf("err = %v, want wrapped", err)
	}
}

func TestUpdatePRBody(t *testing.T) {
	var body map[string]any
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		if got, want := r.URL.Path, "/api/v3/repos/o/r/pulls/7"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mustJSON(w, map[string]any{"number": 7, "body": body["body"]})
	})
	if err := c.UpdatePRBody(context.Background(), "o", "r", 7, "new body text"); err != nil {
		t.Fatalf("UpdatePRBody: %v", err)
	}
	if body["body"] != "new body text" {
		t.Errorf("payload body = %v, want new body text", body["body"])
	}
	// Only the body field should be sent — no title, no state.
	if _, has := body["title"]; has {
		t.Errorf("payload should not include title: %v", body)
	}
}

func TestUpdatePRBody_WrapsError(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"nope"}`))
	})
	err := c.UpdatePRBody(context.Background(), "o", "r", 7, "x")
	if err == nil || !strings.Contains(err.Error(), "gh:") {
		t.Errorf("err = %v, want wrapped", err)
	}
}

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
	err := c.UploadSARIF(context.Background(), "o", "r", "deadbeef", "refs/heads/main", origin)
	if err != nil {
		t.Fatalf("UploadSARIF: %v", err)
	}
	if payload.CommitSHA != "deadbeef" || payload.Ref != "refs/heads/main" || payload.ToolName != "zreview" {
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
	err := c.UploadSARIF(context.Background(), "o", "r", "sha", "refs/heads/main", []byte("{}"))
	if err == nil || !strings.Contains(err.Error(), "gh:") {
		t.Errorf("err = %v, want wrapped", err)
	}
}
