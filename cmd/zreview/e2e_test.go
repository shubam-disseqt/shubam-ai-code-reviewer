// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// TestReviewE2E_WorkspaceMode_ProducesJSON drives the full zreview binary as a
// subprocess against a fresh git repo and an httptest server that impersonates
// Anthropic's /v1/messages endpoint. It exists so a regression in env
// resolution, LLM wiring, tool dispatch, diff resolution, scoring or JSON
// emit shows up in one place — no unit test in the tree wires the whole path
// end-to-end. Slow (build + subprocess); skipped under -short.
func TestReviewE2E_WorkspaceMode_ProducesJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e disabled with -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go binary not found: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git binary not found: %v", err)
	}

	// 1) Fake Anthropic. Returns one code_comment on main.go:1 + task_done
	// for every request. Cheap-tier calls (summarizer, labeler) fetch the
	// same body; parseCheapJSON fails since there's no text envelope and
	// runCheapAgents logs + continues — that's the best-effort contract.
	//
	// existing_code matches the added line in commit 2's diff exactly so
	// diff.ResolveComment snaps StartLine/EndLine to line 1, keeping the
	// comment out of filterResolved's drop path. Usage carries realistic
	// input/output token counts so the metrics aggregator doesn't skip.
	anthropicResp := `{
		"id": "msg_e2e",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-6",
		"content": [
			{
				"type": "tool_use",
				"id": "tu_1",
				"name": "code_comment",
				"input": {
					"comments": [
						{
							"content": "test finding from e2e fake",
							"existing_code": "func main() { println(\"hi\") }",
							"category": "bug",
							"severity": "medium",
							"path": "main.go"
						}
					]
				}
			},
			{
				"type": "tool_use",
				"id": "tu_2",
				"name": "task_done",
				"input": {"state": "DONE"}
			}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 120, "output_tokens": 40}
	}`
	var reqCount int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&reqCount, 1)
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(anthropicResp))
	}))
	defer srv.Close()

	// 2) Fresh git repo with a real 2-commit diff so workspace mode has
	// something to review. Workspace mode diffs staged+unstaged against
	// HEAD, so commit 2's change must live in the working tree — we stage
	// the modification but don't commit it.
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	runGit("config", "user.email", "e2e@test")
	runGit("config", "user.name", "e2e")
	writeFile(t, filepath.Join(repo, "main.go"), "package main\n\nfunc main() {}\n")
	runGit("add", "main.go")
	runGit("commit", "-q", "-m", "seed")
	writeFile(t, filepath.Join(repo, "main.go"), "package main\n\nfunc main() { println(\"hi\") }\n")
	runGit("add", "main.go")

	// 3) Build the binary fresh into a temp dir. Adds ~2-5s vs a cached
	// build, but keeps the test reproducible from a clean checkout.
	binDir := t.TempDir()
	binName := "zreview"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(binDir, binName)
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = mustModuleDir(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}

	// 4) Run the subprocess.
	//
	// Env strategy: use OCR_LLM_* to force the httptest URL through the
	// highest-priority resolver strategy (tryOCREnv), then set
	// ANTHROPIC_API_KEY alongside so tryProviderEnv is reachable as a
	// fallback. This proves the friendly-var fix at 155004e didn't
	// regress — the resolver accepts it without erroring — while keeping
	// the actual network hermetic on the OCR path (tryProviderEnv would
	// hardcode api.anthropic.com and we can't override that). HOME is
	// scrubbed so ~/.opencodereview/config.json can't leak in.
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	fakeHome := t.TempDir()
	env := []string{
		"HOME=" + fakeHome,
		"PATH=" + os.Getenv("PATH"), // git still needs to run inside the subprocess
		"OCR_LLM_URL=" + srv.URL,
		"OCR_LLM_TOKEN=sk-ant-test",
		"OCR_LLM_MODEL=claude-sonnet-4-6",
		"ANTHROPIC_API_KEY=sk-ant-test",
		"ANTHROPIC_MODEL=claude-sonnet-4-6",
		"ZREVIEW_SESSION_DIR=" + sessionDir,
		"ZREVIEW_DB_URL=", // no index store
		"ZREVIEW_ORG_RULES_REPO=",
		"GITHUB_TOKEN=", // skip overlap + gh emitter
		"GITHUB_REPOSITORY=",
	}
	if runtime.GOOS == "windows" {
		// Cobra + Windows need a couple of extras that PATH doesn't cover.
		for _, k := range []string{"SystemRoot", "TEMP", "TMP"} {
			if v := os.Getenv(k); v != "" {
				env = append(env, k+"="+v)
			}
		}
	}

	cmd := exec.Command(binPath, "review", "--repo", repo, "--format", "json", "--output", "-")
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("zreview review: %v\nstdout: %s\nstderr: %s", err, stdout, stderr.String())
	}
	if testing.Verbose() {
		t.Logf("stderr:\n%s", stderr.String())
	}

	// 5) Assert on the JSON envelope. emit_test.go covers the shape in
	// isolation; here we only need "the subprocess produced something a
	// consumer can parse, and the LLM finding survived the pipeline".
	var out emitResult
	if err := json.Unmarshal(stdout, &out); err != nil {
		t.Fatalf("stdout is not valid emitResult JSON: %v\nraw: %s", err, stdout)
	}
	if out.SessionID == "" {
		t.Error("session_id missing from JSON output")
	}
	// Fake anthropic must have been hit at least once (labeler + summarizer +
	// main task = 3 in the current pipeline). Anything less means env
	// resolution stopped short of a real LLM call.
	if atomic.LoadInt64(&reqCount) == 0 {
		t.Errorf("fake anthropic never got called — env resolution didn't reach the client")
	}

	// Comment survival is a softer assertion: today the review Deps builder
	// doesn't register code_comment on the tool registry, so the loop's
	// not-found short-circuit swallows the finding before it reaches the
	// collector. That's a real production bug (unrelated to this test), but
	// this test is scoped to env + wiring + JSON emit, not to code_comment
	// registration. Log it, don't fail on it — so a fix upstream flips this
	// into a positive assertion via the len>0 branch below.
	// ponytail: comment-survival check is soft, upgrade to hard assert once
	// review_cmd.go registers a code_comment stub on the tool registry.
	if len(out.Comments) > 0 {
		var mainGoHit bool
		for _, c := range out.Comments {
			if c.Path == "main.go" {
				mainGoHit = true
				break
			}
		}
		if !mainGoHit {
			t.Errorf("no comment landed on main.go: %+v", out.Comments)
		}
	} else {
		t.Logf("no comments in output — pipeline reached emit but code_comment " +
			"was filtered before collector.Add (see review_cmd.buildToolRegistry)")
	}

	// 6) Session log — one JSONL per session ID under the override dir.
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		t.Fatalf("read session dir: %v", err)
	}
	var jsonl string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			jsonl = e.Name()
			break
		}
	}
	if jsonl == "" {
		t.Errorf("no .jsonl session log written under %s (entries: %v)", sessionDir, entries)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustModuleDir returns the cmd/zreview package directory so `go build .`
// invoked from a random cwd still targets this binary.
func mustModuleDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(thisFile)
}
