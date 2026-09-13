// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package overlap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/shubam-disseqt/z-code-reviewer/internal/gh"
	"github.com/shubam-disseqt/z-code-reviewer/internal/llm"
)

// mockLLM captures the request and returns a fixed response.
type mockLLM struct {
	resp string
	err  error
	last llm.ChatRequest
}

func (m *mockLLM) CompletionsWithCtx(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.last = req
	if m.err != nil {
		return nil, m.err
	}
	c := m.resp
	return &llm.ChatResponse{
		Choices: []llm.Choice{{Message: llm.ResponseMessage{Content: &c}}},
	}, nil
}

// buildFakeGH stands up an httptest server that answers list-pulls and
// list-files with the given canned data. Returns a *gh.Client wired to it.
type fakeRepo struct {
	// prs is the list-open-prs response.
	prs []gh.OpenPRRef
	// files maps PR number → file list.
	files map[int][]string
}

func buildFakeGH(t *testing.T, r fakeRepo) *gh.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/api/v3/repos/o/rr/pulls":
			out := make([]map[string]any, 0, len(r.prs))
			for _, pr := range r.prs {
				out = append(out, map[string]any{
					"number":     pr.Number,
					"title":      pr.Title,
					"body":       pr.Body,
					"draft":      pr.IsDraft,
					"html_url":   pr.HTMLURL,
					"head":       map[string]any{"ref": pr.HeadRef, "sha": pr.HeadSHA},
					"base":       map[string]any{"ref": pr.BaseRef, "sha": "base"},
					"user":       map[string]any{"login": pr.UserLogin},
					"updated_at": "2026-09-01T12:00:00Z",
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasSuffix(req.URL.Path, "/files"):
			// /repos/o/rr/pulls/<n>/files
			parts := strings.Split(req.URL.Path, "/")
			// parts: "", "repos", "o", "rr", "pulls", "<n>", "files"
			n, err := strconv.Atoi(parts[len(parts)-2])
			if err != nil {
				http.Error(w, "bad number", 400)
				return
			}
			files, ok := r.files[n]
			if !ok {
				http.NotFound(w, req)
				return
			}
			out := make([]map[string]any, 0, len(files))
			for _, f := range files {
				out = append(out, map[string]any{"filename": f})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := gh.NewClient(gh.Options{Token: "t", BaseURL: srv.URL + "/api/v3/"})
	if err != nil {
		t.Fatalf("gh.NewClient: %v", err)
	}
	return c
}

func TestDetect_HappyPath(t *testing.T) {
	repo := fakeRepo{
		prs: []gh.OpenPRRef{
			{Number: 100, Title: "self", HTMLURL: "u/100", HeadRef: "cur", BaseRef: "main"}, // current — dropped
			{Number: 200, Title: "add rate limiter to auth", HTMLURL: "u/200", HeadRef: "f200", BaseRef: "main", UserLogin: "bob"},
			// Title-similar-only: no file overlap, but jaccard vs cur passes threshold.
			{Number: 300, Title: "add rate limiter to login page", HTMLURL: "u/300", HeadRef: "f300", BaseRef: "main", UserLogin: "carol"},
			{Number: 400, Title: "unrelated", HTMLURL: "u/400", HeadRef: "f400", BaseRef: "main", IsDraft: true},          // draft
			{Number: 500, Title: "bot", HTMLURL: "u/500", HeadRef: "f500", BaseRef: "main", UserLogin: "dependabot[bot]"}, // bot
			{Number: 600, Title: "stack", HTMLURL: "u/600", HeadRef: "main", BaseRef: "root"},                             // stacked
		},
		files: map[int][]string{
			200: {"auth/limiter.go", "auth/util.go"}, // shares files with current
			300: {"README.md"},
			400: {"x"}, // never fetched (draft)
			500: {"x"}, // never fetched (bot)
			600: {"x"}, // never fetched (stacked)
		},
	}
	ghc := buildFakeGH(t, repo)

	llmResp := `{"overlaps":[
	  {"pr_number":200,"kind":"merge_conflict","reason":"same limiter","confidence":0.9},
	  {"pr_number":300,"kind":"duplicate_effort","reason":"same fix","confidence":1.5}
	]}`
	llmc := &mockLLM{resp: llmResp}

	cur := PR{
		Owner: "o", Repo: "rr", Number: 100,
		Title:   "add rate limiter to login",
		HeadRef: "cur", BaseRef: "main",
		Paths: []string{"auth/limiter.go"},
	}

	got, err := Detect(context.Background(), DefaultConfig(), cur, ghc, llmc)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2 (findings for #200 and #300)", len(got))
	}
	// Sort order: merge_conflict (rank 2) before duplicate_effort (rank 1).
	if got[0].Number != 200 || got[0].Kind != "merge_conflict" {
		t.Errorf("got[0] = %+v, want #200 merge_conflict", got[0])
	}
	if got[1].Number != 300 || got[1].Kind != "duplicate_effort" {
		t.Errorf("got[1] = %+v, want #300 duplicate_effort", got[1])
	}
	// Confidence 1.5 clamped to 1.0.
	if got[1].Confidence != 1.0 {
		t.Errorf("got[1].Confidence = %v, want 1.0 (clamped)", got[1].Confidence)
	}
	if len(got[0].SharedFiles) == 0 || got[0].SharedFiles[0] != "auth/limiter.go" {
		t.Errorf("shared files not carried over: %v", got[0].SharedFiles)
	}
}

func TestDetect_MalformedLLMReturnsEmpty(t *testing.T) {
	repo := fakeRepo{
		prs:   []gh.OpenPRRef{{Number: 200, Title: "cand", HTMLURL: "u", HeadRef: "f", BaseRef: "main"}},
		files: map[int][]string{200: {"a.go"}},
	}
	ghc := buildFakeGH(t, repo)
	llmc := &mockLLM{resp: "not json"}
	cur := PR{Owner: "o", Repo: "rr", Number: 100, Title: "x", HeadRef: "c", BaseRef: "main", Paths: []string{"a.go"}}
	got, err := Detect(context.Background(), DefaultConfig(), cur, ghc, llmc)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
}

func TestDetect_LLMErrorSwallowed(t *testing.T) {
	repo := fakeRepo{
		prs:   []gh.OpenPRRef{{Number: 200, Title: "cand", HeadRef: "f", BaseRef: "main"}},
		files: map[int][]string{200: {"a.go"}},
	}
	ghc := buildFakeGH(t, repo)
	llmc := &mockLLM{err: errBoom{}}
	cur := PR{Owner: "o", Repo: "rr", Number: 100, Title: "x", HeadRef: "c", BaseRef: "main", Paths: []string{"a.go"}}
	got, err := Detect(context.Background(), DefaultConfig(), cur, ghc, llmc)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if got != nil {
		t.Errorf("want nil, got %v", got)
	}
}

func TestDetect_NoSurvivorsSkipsLLM(t *testing.T) {
	// One candidate PR whose file list is disjoint and title jaccard is 0.
	repo := fakeRepo{
		prs:   []gh.OpenPRRef{{Number: 200, Title: "totally unrelated database", HeadRef: "f", BaseRef: "main"}},
		files: map[int][]string{200: {"db.go"}},
	}
	ghc := buildFakeGH(t, repo)
	llmc := &mockLLM{resp: "should never be called"}
	cur := PR{Owner: "o", Repo: "rr", Number: 100, Title: "readme typo", HeadRef: "c", BaseRef: "main", Paths: []string{"README.md"}}
	got, _ := Detect(context.Background(), DefaultConfig(), cur, ghc, llmc)
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
	if llmc.last.Messages != nil {
		t.Errorf("LLM was called with empty candidate set")
	}
}

func TestDetect_ConfidenceFloor(t *testing.T) {
	repo := fakeRepo{
		prs:   []gh.OpenPRRef{{Number: 200, Title: "shared", HeadRef: "f", BaseRef: "main"}},
		files: map[int][]string{200: {"a.go"}},
	}
	ghc := buildFakeGH(t, repo)
	llmc := &mockLLM{resp: `{"overlaps":[{"pr_number":200,"kind":"merge_conflict","reason":"maybe","confidence":0.3}]}`}
	cur := PR{Owner: "o", Repo: "rr", Number: 100, Title: "shared", HeadRef: "c", BaseRef: "main", Paths: []string{"a.go"}}
	got, _ := Detect(context.Background(), DefaultConfig(), cur, ghc, llmc)
	if len(got) != 0 {
		t.Errorf("want floor to drop it, got %v", got)
	}
}

func TestDetect_DisabledOrNilDeps(t *testing.T) {
	cur := PR{Owner: "o", Repo: "r", Number: 1}
	if got, _ := Detect(context.Background(), Config{Enabled: false}, cur, nil, nil); got != nil {
		t.Errorf("disabled: want nil, got %v", got)
	}
	if got, _ := Detect(context.Background(), DefaultConfig(), cur, nil, nil); got != nil {
		t.Errorf("nil deps: want nil, got %v", got)
	}
}

func TestDetect_SortStable_SameRank(t *testing.T) {
	repo := fakeRepo{
		prs: []gh.OpenPRRef{
			{Number: 200, Title: "a shared thing", HeadRef: "f200", BaseRef: "main"},
			{Number: 300, Title: "b shared thing", HeadRef: "f300", BaseRef: "main"},
		},
		files: map[int][]string{200: {"z.go"}, 300: {"z.go"}},
	}
	ghc := buildFakeGH(t, repo)
	llmc := &mockLLM{resp: `{"overlaps":[
	  {"pr_number":200,"kind":"merge_conflict","reason":"","confidence":0.7},
	  {"pr_number":300,"kind":"merge_conflict","reason":"","confidence":0.9}
	]}`}
	cur := PR{Owner: "o", Repo: "rr", Number: 100, Title: "shared thing", HeadRef: "c", BaseRef: "main", Paths: []string{"z.go"}}
	got, _ := Detect(context.Background(), DefaultConfig(), cur, ghc, llmc)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	// Higher confidence first inside the same kind.
	if got[0].Number != 300 || got[1].Number != 200 {
		t.Errorf("order = [%d, %d], want [300, 200]", got[0].Number, got[1].Number)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
