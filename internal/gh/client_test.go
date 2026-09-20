// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira src/mira/core/overlap.py
// under Apache License 2.0.

package gh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// newFakeGitHub returns an httptest server whose handler is `h`, plus a
// *Client wired to hit it. We suffix "/api/v3/" so WithEnterpriseURLs doesn't
// silently splice its own prefix in — matches how a real GHES host looks.
func newFakeGitHub(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(Options{Token: "test-token", BaseURL: srv.URL + "/api/v3/"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

func TestListOpenPRs_Paginated(t *testing.T) {
	var calls int32
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if got, want := r.URL.Path, "/api/v3/repos/o/rr/pulls"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if r.URL.Query().Get("state") != "open" {
			t.Errorf("state = %q, want open", r.URL.Query().Get("state"))
		}
		if r.URL.Query().Get("sort") != "updated" {
			t.Errorf("sort = %q, want updated", r.URL.Query().Get("sort"))
		}
		if r.URL.Query().Get("direction") != "desc" {
			t.Errorf("direction = %q, want desc", r.URL.Query().Get("direction"))
		}
		if h := r.Header.Get("Authorization"); !strings.HasPrefix(h, "Bearer ") {
			t.Errorf("Authorization = %q, want Bearer prefix", h)
		}
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			// Point at page 2 via Link header — go-github parses this.
			w.Header().Set("Link", `<http://`+r.Host+r.URL.Path+`?page=2>; rel="next"`)
			mustJSON(w, []map[string]any{
				prJSON(1, "first"),
				prJSON(2, "second"),
			})
			return
		}
		// Page 2: one PR, no more links.
		mustJSON(w, []map[string]any{prJSON(3, "third")})
	})

	prs, err := c.ListOpenPRs(context.Background(), "o", "rr", 5)
	if err != nil {
		t.Fatalf("ListOpenPRs: %v", err)
	}
	if len(prs) != 3 {
		t.Fatalf("len = %d, want 3", len(prs))
	}
	if prs[0].Number != 1 || prs[0].Title != "first" {
		t.Errorf("prs[0] = %+v", prs[0])
	}
	if prs[2].Number != 3 {
		t.Errorf("prs[2].Number = %d, want 3", prs[2].Number)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("calls = %d, want 2 (paginated)", calls)
	}
}

func TestListOpenPRs_LimitZero(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP call")
	})
	prs, err := c.ListOpenPRs(context.Background(), "o", "r", 0)
	if err != nil || prs != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", prs, err)
	}
}

func TestListOpenPRs_StopsAtLimit(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+r.URL.Scheme+"://"+r.Host+r.URL.Path+`?page=2>; rel="next"`)
		mustJSON(w, []map[string]any{
			prJSON(1, "a"), prJSON(2, "b"), prJSON(3, "c"),
		})
	})
	prs, err := c.ListOpenPRs(context.Background(), "o", "r", 2)
	if err != nil {
		t.Fatalf("ListOpenPRs: %v", err)
	}
	if len(prs) != 2 {
		t.Errorf("len = %d, want 2", len(prs))
	}
}

func TestGetPRFiles_200(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v3/repos/o/r/pulls/42/files"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		mustJSON(w, []map[string]any{
			{"filename": "a.go"}, {"filename": "b.go"},
		})
	})
	files, err := c.GetPRFiles(context.Background(), "o", "r", 42, 100)
	if err != nil {
		t.Fatalf("GetPRFiles: %v", err)
	}
	if len(files) != 2 || files[0] != "a.go" || files[1] != "b.go" {
		t.Errorf("files = %v", files)
	}
}

func TestGetPRFiles_404(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})
	files, err := c.GetPRFiles(context.Background(), "o", "r", 999, 100)
	if err != nil {
		t.Fatalf("GetPRFiles: want nil err on 404, got %v", err)
	}
	if files != nil {
		t.Errorf("files = %v, want nil", files)
	}
}

func TestGetPRFiles_500Wraps(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	})
	_, err := c.GetPRFiles(context.Background(), "o", "r", 1, 10)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "gh:") {
		t.Errorf("err = %v, want gh: prefix", err)
	}
}

func TestGetPRFiles_LimitZero(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected call")
	})
	files, err := c.GetPRFiles(context.Background(), "o", "r", 1, 0)
	if err != nil || files != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", files, err)
	}
}

func TestGetPRFiles_StopsAtLimit(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		mustJSON(w, []map[string]any{
			{"filename": "a"}, {"filename": "b"}, {"filename": "c"},
		})
	})
	files, err := c.GetPRFiles(context.Background(), "o", "r", 1, 2)
	if err != nil {
		t.Fatalf("GetPRFiles: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("len = %d, want 2", len(files))
	}
}

func TestPostReviewComment_SingleLine(t *testing.T) {
	var body map[string]any
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got, want := r.URL.Path, "/api/v3/repos/o/r/pulls/7/comments"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	err := c.PostReviewComment(context.Background(), "o", "r", 7, ReviewComment{
		Path: "a.go", Body: "hi", Line: 10, CommitSHA: "sha",
	})
	if err != nil {
		t.Fatalf("PostReviewComment: %v", err)
	}
	if body["path"] != "a.go" || body["body"] != "hi" {
		t.Errorf("body = %v", body)
	}
	if _, hasStart := body["start_line"]; hasStart {
		t.Errorf("single-line comment must not send start_line, body = %v", body)
	}
	if body["side"] != "RIGHT" {
		t.Errorf("side = %v, want RIGHT (default)", body["side"])
	}
}

func TestPostReviewComment_MultiLine(t *testing.T) {
	var body map[string]any
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	err := c.PostReviewComment(context.Background(), "o", "r", 7, ReviewComment{
		Path: "a.go", Body: "range", Line: 20, StartLine: 15, Side: "LEFT", CommitSHA: "sha",
	})
	if err != nil {
		t.Fatalf("PostReviewComment: %v", err)
	}
	if body["start_line"].(float64) != 15 || body["line"].(float64) != 20 {
		t.Errorf("lines wrong, body = %v", body)
	}
	if body["side"] != "LEFT" {
		t.Errorf("side = %v, want LEFT", body["side"])
	}
}

func TestPostReviewComment_ErrorWraps(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"bad line"}`))
	})
	err := c.PostReviewComment(context.Background(), "o", "r", 7, ReviewComment{
		Path: "a.go", Body: "hi", Line: 10, CommitSHA: "sha",
	})
	if err == nil || !strings.Contains(err.Error(), "gh:") {
		t.Errorf("err = %v, want wrapped", err)
	}
}

func TestNewClient_NoTokenNoBase(t *testing.T) {
	c, err := NewClient(Options{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.authed {
		t.Errorf("authed = true, want false")
	}
}

func TestNewClient_InvalidEnterpriseURL(t *testing.T) {
	// go-github rejects a control char in the URL when it builds the parsed base.
	_, err := NewClient(Options{BaseURL: "http://\x7f/"})
	if err == nil {
		t.Fatal("want error for bad URL")
	}
}

func TestPostIssueComment(t *testing.T) {
	var body map[string]any
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got, want := r.URL.Path, "/api/v3/repos/o/r/issues/9/comments"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":123,"body":"hello"}`))
	})
	got, err := c.PostIssueComment(context.Background(), "o", "r", 9, "hello")
	if err != nil {
		t.Fatalf("PostIssueComment: %v", err)
	}
	if got.ID != 123 || got.Body != "hello" {
		t.Errorf("got = %+v, want {123 hello}", got)
	}
	if body["body"] != "hello" {
		t.Errorf("request body = %v", body)
	}
}

func TestPostIssueComment_WrapsError(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"nope"}`))
	})
	_, err := c.PostIssueComment(context.Background(), "o", "r", 9, "x")
	if err == nil || !strings.Contains(err.Error(), "gh:") {
		t.Errorf("err = %v, want wrapped", err)
	}
}

func TestListIssueComments(t *testing.T) {
	var calls int32
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if got, want := r.URL.Path, "/api/v3/repos/o/r/issues/4/comments"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			w.Header().Set("Link", `<http://`+r.Host+r.URL.Path+`?page=2>; rel="next"`)
			mustJSON(w, []map[string]any{
				{"id": 1, "body": "one"},
				{"id": 2, "body": "two"},
			})
			return
		}
		mustJSON(w, []map[string]any{{"id": 3, "body": "three"}})
	})
	out, err := c.ListIssueComments(context.Background(), "o", "r", 4)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(out) != 3 || out[0].ID != 1 || out[2].Body != "three" {
		t.Errorf("out = %+v", out)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("calls = %d, want 2 (paginated)", calls)
	}
}

func TestDeleteIssueComment(t *testing.T) {
	var seen struct {
		method, path string
	}
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		seen.method, seen.path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.DeleteIssueComment(context.Background(), "o", "r", 55); err != nil {
		t.Fatalf("DeleteIssueComment: %v", err)
	}
	if seen.method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", seen.method)
	}
	if seen.path != "/api/v3/repos/o/r/issues/comments/55" {
		t.Errorf("path = %q", seen.path)
	}
}

func TestDeleteIssueComment_404OK(t *testing.T) {
	c, _ := newFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})
	if err := c.DeleteIssueComment(context.Background(), "o", "r", 55); err != nil {
		t.Errorf("want nil err on 404, got %v", err)
	}
}

// --- helpers ---

func prJSON(number int, title string) map[string]any {
	return map[string]any{
		"number":   number,
		"title":    title,
		"body":     "",
		"draft":    false,
		"html_url": "https://example.test/pull/" + itoa(number),
		"head": map[string]any{
			"ref": "feature-" + itoa(number),
			"sha": "sha-" + itoa(number),
		},
		"base": map[string]any{
			"ref": "main",
			"sha": "basesha",
		},
		"user":       map[string]any{"login": "alice"},
		"updated_at": "2026-09-01T12:00:00Z",
	}
}

func mustJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
