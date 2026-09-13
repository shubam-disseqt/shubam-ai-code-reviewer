# Porting Spec

Synthesized from the Phase 1 research pass. Five parallel agents mapped
the two upstream projects onto our target Go layout. This file is the
per-file plan for Phases 3–8.

**Reference clones (not tracked in this repo, cloned locally by Phase 1):**
- `alibaba/open-code-review` — Go, Apache-2.0
- `miracodeai/mira` — Python, Apache-2.0

Both are Apache-2.0. Attribution is set out in [NOTICE](NOTICE) and each
lifted file carries the upstream copyright header verbatim per §4 of the
license.

---

## 1. Provenance verdict per source file

Verdicts:
- **COPY** — Go source, take verbatim, adjust import paths only.
- **COPY+TRIM** — Go source, take but delete OCR-specific deps (session, delegation, viewer, telemetry).
- **PORT** — Python source, re-implement in Go with the same semantics.
- **REWRITE** — logic worth keeping but the Python or Go shape does not fit; write fresh from spec.
- **SKIP** — do not lift.

### From `alibaba/open-code-review` (Go — COPY / COPY+TRIM)

| Upstream path | LOC | Verdict | Target |
|---|---|---|---|
| `internal/diff/parser.go` | 150 | COPY | `internal/diff/parser.go` |
| `internal/diff/hunk.go` | 113 | COPY | `internal/diff/hunk.go` |
| `internal/diff/resolver.go` | 306 | COPY | `internal/diff/resolver.go` — **line-snapping algorithm, highest value** |
| `internal/diff/relocation.go` | 117 | COPY+TRIM | `internal/diff/relocation.go` — drop `ReLocateComment` (drags in `llm`, `template`, `stdout`); keep `extractCodeBlock` + `RelocateAcrossFiles` |
| `internal/diff/git.go` | 728 | COPY | `internal/diff/git.go` — includes remote canonicalization + untracked-file diff synthesis (feature, not just parsing) |
| `internal/diff/gitignore.go` | 36 | COPY | `internal/diff/gitignore.go` |
| `internal/diff/workspace_file.go` | 63 | COPY | `internal/diff/workspace_file.go` |
| `internal/pathutil/path.go` | 53 | COPY | `internal/pathutil/path.go` |
| `internal/gitcmd/runner.go` | 130 | COPY | `internal/gitcmd/runner.go` |
| `internal/tool/code_comment.go` | 177 | COPY+TRIM | `internal/comment/parse.go` — strip `Tool` shell + `Execute()`; keep `ParseComments` + normalization |
| `internal/tool/comment_args_repair.go` | 291 | COPY | `internal/comment/args_repair.go` — the JSON repair pass |
| `internal/tool/comment_collector.go` | 116 | COPY | `internal/comment/collector.go` |
| `internal/agent/selection.go` | 121 | COPY+TRIM | `internal/select/selection.go` — swap `llmloop.PromptTokenLimit`/`llm.CountTokens` for interfaces |
| `internal/agent/grouping.go` | 391 | REWRITE | `internal/bundle/grouping.go` — big rewrite (drops session, telemetry, template deps); **skip for v1**, use one-file-per-review |
| `internal/llm/*` | ~2000 | COPY+TRIM | `internal/llm/*` — trim providers table to Anthropic / OpenAI / Bedrock / Gemini / DeepSeek |
| `internal/llmloop/loop.go` | 500+ | COPY+TRIM | `internal/llmloop/loop.go` — drop retry-report identity ledger, keep three-zone thresholds |
| `internal/llmloop/compression.go` | ~250 | COPY | `internal/llmloop/compression.go` — three-zone algorithm (60% async / 80% sync) |
| `internal/llmloop/pool.go` | ~130 | COPY | `internal/llmloop/pool.go` — `CommentWorkerPool` with per-key WGs |
| `internal/tool/{file_read,file_find,code_search,filereader,file_read_diff,response_message,stub,definitions}.go` | ~700 | COPY | `internal/tool/*.go` |
| `internal/config/toolsconfig/tools.json` | 214 | COPY | `internal/tool/tools.json` (embed; drop `plan_task` field) |
| `internal/session/{persist,manifest,resume}.go` | ~800 | COPY+TRIM | `internal/session/*.go` — keep JSONL writer + resume-by-fingerprint; drop viewer, drop `list.go` / `compare.go` / `raw_writer.go` |
| `internal/viewer/{hostguard,securityheaders}.go` | ~200 | COPY | `internal/docs/{hostguard,securityheaders}.go` — DNS-rebinding defense for `zreview docs` |
| `internal/config/template/prompts/main_task_*.md` | — | COPY | `internal/prompts/*.md` |
| `internal/config/template/prompts/memory_compression_task_*.md` | — | COPY | `internal/prompts/*.md` |

**Total from OCR: ~5,000 LOC production + ~3,400 LOC tests.**

### From `alibaba/open-code-review` — SKIP

| Upstream path | Why not |
|---|---|
| `internal/mcp/*` | MCP server; out of scope. |
| `internal/delegate/*` | Delegation mode; out of scope. |
| `internal/scan/*` | Full-file scan; may return as v2 optional. |
| `internal/agent/{agent,preview,estimate,identity,util}.go` | Agent lifecycle + session identity + resume manifests. Not the diff-precision layer. |
| `internal/session/{history,compare,list,raw_writer}.go` | Viewer surface. |
| `internal/viewer/*` (beyond hostguard + securityheaders) | Session-history browser; wrong domain. |
| `retry_report.go`, `retry_meta.go`, `retry_observer.go`, `retry_boundary.go`, `sessionkey.go`, `usage_resolver.go` | Attempt-ledger for retry telemetry — ~1500 LOC. Provider SDKs already retry with `WithMaxRetries(5)`. |
| `cmd/opencodereview/{delegate,viewer}_cmd.go` | Corresponding CLI commands. |

### From `miracodeai/mira` (Python → PORT to Go)

| Upstream path | LOC | Verdict | Target |
|---|---|---|---|
| `src/mira/index/indexer.py` | 974 | PORT | `internal/index/indexer.go` (~600 LOC) + `internal/index/batch.go` (~100) + `internal/index/summarize.go` (~250) |
| `src/mira/index/store.py` | 1828 | PORT (schema); REWRITE (types) | `internal/index/sqlite_store.go` (~700) + `internal/index/schema.go` (~200) + `internal/index/types.go` (~200). Drop review_events / feedback / learned_rules / vulnerabilities tables — Mira dashboard concerns, out of scope. |
| `src/mira/index/pg_store.py` | 1750 | PORT | `internal/index/postgres_store.go` (~800). Adds `(owner, repo)` prefix to every PK. |
| `src/mira/index/_store_shared.py` | 58 | REWRITE | `internal/index/store.go` — Go interface + shared methods (~150 LOC). |
| `src/mira/index/jit_context.py` | 505 | PORT | `internal/context/jit.go` (~500) + `internal/context/imports.go` (~300 per-language import resolvers). |
| `src/mira/index/context.py` | 276 | PORT | `internal/context/review.go` (~250) — 60/30/10 token budget across source / summaries / dirs. |
| `src/mira/index/manifests.py` | 504 | PORT | `internal/manifests/*.go` (~600 total) — one file per manifest kind. Use Go-native libs where available (`x/mod/modfile` for `go.mod`). |
| `src/mira/index/extract.py` | 446 | PORT | `internal/extract/extract.go` (~450) — symbol regex table + brace/indent walkers. |
| `src/mira/index/conventions.py` | 114 | PORT | `internal/conventions/conventions.go` (~120). |
| `src/mira/core/file_types.py` | 155 | PORT | `internal/filetype/filetype.go` (~150) — `Language` enum + `IsIndexablePath`. |
| `src/mira/core/file_filter.py` | 73 | PORT | `internal/filter/filter.go` (~80). |
| `src/mira/core/context.py` | 83 | PORT | `internal/context/hunk.go` (~80) — hunk merger + diff→markdown for prompt. |
| `src/mira/core/chunker.py` | 107 | PORT | `internal/chunker/chunker.go` (~120) — token-aware first-fit-decreasing. |
| `src/mira/index/status.py` | 93 | REWRITE | `internal/index/status.go` — small thread-safe progress tracker. |
| `src/mira/core/overlap.py` | ~400 | PORT | `internal/overlap/overlap.go` + `prefilter.go` + `prompt.go` + `parse.go` + `render.go` (~500 total). |
| `src/mira/core/noise_filter.py` (`_jaccard_similarity` only) | ~20 | PORT | Inline into `internal/overlap/prefilter.go`. |
| `src/mira/analysis/severity.py` | ~100 | PORT | `internal/comment/severity.go` — severity ordering, blocker forces "do not merge". |
| `src/mira/llm/prompts/overlap.py` | ~80 | PORT | `internal/prompts/overlap.md` (template) + `internal/overlap/prompt.go` (builder). |
| `src/mira/llm/prompts/templates/summarize.jinja2` | 57 | COPY | `internal/prompts/summarize.md` — verbatim, switch engine to Go `text/template`. |
| `src/mira/llm/prompts/templates/review.jinja2` (rules block only) | ~30 | COPY | `internal/prompts/rules_block.md` — the `## Custom Review Rules` fragment. |
| `src/mira/models.py` (relevant dataclasses) | ~200 | PORT | `internal/gh/types.go` — `PRFingerprint`, `OpenPRRef`, `OverlapFinding`, `PRInfo`. |

**Total from Mira: ~6,500 LOC Go (from ~7,000 LOC Python).**

### From `miracodeai/mira` — SKIP

| Upstream path | Why not |
|---|---|
| `src/mira/analysis/feedback.py` | Learning loop — explicitly out of scope. |
| `src/mira/security/` (OSV poller) | Vulnerability scanning — out of scope. |
| `src/mira/dashboard/` | Dashboard — out of scope. |
| `src/mira/index/relationships.py` (765 LOC) | Cross-repo clustering; Mira dashboard concern. |
| `src/mira/core/{engine,ensemble,noise_filter (beyond Jaccard),passes,threads,review_status,priority}.py` | Server-side orchestration; we replace with the Go pipeline. |
| `src/mira/platforms/{gitlab,forgejo}.py` | GitHub-only for v1. |
| `src/mira/outbound_webhooks.py` | Webhook egress — the tool is not a service. |

---

## 2. Attribution model (per-file header)

Per Apache-2.0 §4, files copied from OCR retain their original header
verbatim and get an added portion-copyright line:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt — adapted from alibaba/open-code-review
//
// Adapted from alibaba/open-code-review@<upstream-sha> <original-path>

package diff
```

Files ported from Mira (rewritten in Go, not copied) get:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt
//
// Semantics ported from miracodeai/mira@<upstream-sha> <original-path>

package index
```

CI check enforces both headers on any file under packages the porting
spec lists (`scripts/verify-attribution.go`).

---

## 3. Key algorithms called out

Three algorithms deserve special care because they are the load-bearing
correctness stories:

### 3.1 Line-snapping (OCR — `internal/diff/resolver.go`)

Takes an LLM comment with an `existing_code` anchor and produces
`(StartLine, EndLine)` on the new side of the diff:

1. Skip if already positioned.
2. `splitAndNormalize(existing_code)` — trim, strip diff markers, drop empty lines.
3. Try new-side hunk match: for each hunk, extract `(newLineNumber, content)` pairs for context+added lines; sliding-window match.
4. On miss, try old side (context+deleted).
5. On miss, scan `NewFileContent` line-by-line, matching normalized non-blank lines.
6. On miss, `RelocateAcrossFiles` returns a unique cross-file hit or `("", false)`.
7. On miss, caller can invoke a second LLM re-location pass (we skip this v1 — measured need).

### 3.2 JIT context (Mira — `jit_context.py`)

Runs when the index is empty for the files being reviewed:

1. Fetch each changed file's source at HEAD.
2. Per-language regex extracts imports (Python `_PY_IMPORT_RE`, JS `_JS_IMPORT_RE`, Ruby, Java, Go).
3. Resolve import → candidate repo-relative paths. Go with a `go.mod` is deterministic (`module + "/" + rel_dir`); without it, tail-match heuristic.
4. Filter candidates against `repo_tree` set — skip fetches for paths that don't exist.
5. Fetch survivors (cap 8), run `extract_symbols`, inline symbol source. Per-file cap 1500 chars; total budget 12000 chars.
6. Output markdown injected into the review prompt.

The Go-specific import resolver is a state machine over lines, not a
regex — Go source has quoted strings everywhere. Port the state
machine, not the regex.

### 3.3 Overlap prefilter (Mira — `core/overlap.py`)

Cheap gate before the LLM sees any candidate PR:

```
def prefilter(current, candidate, title_threshold=0.4):
    shared_files = set(current.paths) & set(candidate.paths)
    if shared_files:                                             return keep
    if set(current.symbols) & set(candidate.symbols):            return keep
    if jaccard(current.title, candidate.title) >= threshold:    return keep
    return drop
```

Stacked-branch suppression (`_is_stacked`) drops any candidate whose
head or base ref matches the current PR's — stacked PRs share files by
design and flagging them is noise.

Batched LLM verdict then classifies survivors as `merge_conflict`,
`duplicate_effort`, `both`, or `none`, with a confidence 0..1. Confidence
< 0.6 is dropped.

---

## 4. Concurrency and cancellation

| Location | Pattern |
|---|---|
| Indexer batches | `errgroup.Group` with `SetLimit(8)` (matches Python `_LLM_SEMAPHORE`). Per-batch errors swallowed inside goroutines — one bad batch does not abort the run. Only `context.Canceled` surfaces. |
| File fetches | separate `errgroup` with `SetLimit(10)` (`_FILE_FETCH_SEMAPHORE`). |
| Comment post-processing | `internal/llmloop/pool.go` `CommentWorkerPool` with per-taskKey `sync.WaitGroup` (matches OCR). Panic recovery per unit, logged, counted as zero comments. |
| LLM retry | Provider SDK layer (`WithMaxRetries(5)`) + one bonus retry on `io.ErrUnexpectedEOF` at the transport boundary. No loop-level retry. |
| Overlap | Runs on a `errgroup.Group` sibling to the main review — swallowed on error, never blocks the review. |

Cancellation flows via `context.Context` through every stage. The one
exception is compression — `context.WithoutCancel(ctx)` — because a
cancelled main context should not kill in-flight compression, only its
own `cancelPendingCompression` should.

---

## 5. Modification-required list (what we owe on lift)

1. **Import path rewrite** — `github.com/alibaba/open-code-review/internal/...` → our module path. Mechanical `find ... -exec sed -i` after copy.
2. **`internal/model` split** — OCR bundles `Diff`, `LlmComment`, and other shared types in one `model` package. We split by domain: `internal/diff/types.go` for `Diff`, `internal/comment/types.go` for `LlmComment`.
3. **Provider trim** — `internal/llm/providers.go` from 28 built-in presets to 5 (Anthropic, OpenAI, Bedrock, Gemini, DeepSeek). Anything else via `openai-responses` protocol + custom `BaseURL`.
4. **Session simplification** — drop the retry-report attempt-ledger tables (`retry_report.go` + friends). Provider-SDK retries are enough.
5. **Grouping rewrite** — `internal/bundle/grouping.go` is a full rewrite because OCR's version drags in `session.RunIdentity`, `telemetry`, `template.LlmConversation`. **Defer to v2** — v1 uses one-file-per-review dispatch.
6. **Rules layer greenfield** — Mira's per-repo `review_context` table + `learned_rules` table are collapsed into a single YAML-in-git format. Scope filtering (currently dead in Mira — see [PORTING.md](PORTING.md) §Rules gotchas) is wired up here.
7. **Postgres schema** — Mira's Postgres uses `DOUBLE PRECISION` for timestamps (matches SQLite float epoch). We use `TIMESTAMPTZ` — cleaner for greenfield.
8. **Error strings** — OCR-branded `[ocr]` prefix in a few user-facing errors (`parser.go:137,147`, `relocation.go:73`, `grouping.go:102,148`). Search-replace to `[zreview]` or route through a logger.
9. **Third-party dep pins** — `github.com/bmatcuk/doublestar/v4` (MIT, for `**` globs), `github.com/spf13/cobra` (Apache-2.0, CLI), `github.com/jackc/pgx/v5` (MIT), `modernc.org/sqlite` (BSD-3-clause), `github.com/google/go-github/v63` (BSD-3-clause), `github.com/BurntSushi/toml` or `github.com/pelletier/go-toml/v2` (MIT), `golang.org/x/mod` (BSD-3), `sigs.k8s.io/yaml` or `github.com/goccy/go-yaml`.

---

## 6. Test surface to lift alongside

OCR ships thorough tests we take for free. Priority for the copy:

| Upstream test file | LOC | Guards |
|---|---|---|
| `internal/diff/resolver_test.go` | 880 | **Line-snapping regression harness** — copy first, run second |
| `internal/tool/comment_args_repair_test.go` | (large) | JSON-repair harness |
| `internal/diff/git_test.go` | 540 | Workspace / commit / range providers |
| `internal/diff/parser_test.go` | 274 | Unified-diff parser edge cases |
| `internal/diff/gitignore_test.go` | 250 | Gitignore semantics |
| `internal/diff/git_error_test.go` | 269 | Error surface |
| `internal/diff/git_resolve_test.go` | 225 | Remote canonicalization |
| `internal/pathutil/path_test.go` | 201 | Escape + SameFile fallback |
| `internal/gitcmd/runner_test.go` | 154 | Concurrency limiter |
| `internal/diff/workspace_file_test.go` | 147 | Symlink + escape guards |
| `internal/diff/relocation_test.go` | 297 | Fenced-block extraction (v1 keeps only `extractCodeBlock` + `RelocateAcrossFiles` tests) |
| `internal/diff/hunk_test.go` | 123 | Hunk header regex |

For Mira ports there is no test transfer — Python tests do not port
useful signal. Ported packages get fresh Go tests via table-driven
patterns against a fixed corpus of `refs/mira/tests/fixtures/*` where
Mira ships fixtures.

---

## 7. Gotchas — the non-obvious stuff

Distilled from the Phase 1 "Surprises" sections. Read this before writing
any code in the corresponding package.

- **`content_hash` guards re-indexing.** SHA-256 of the file's bytes (with UTF-8 replace on decode error → stable). Skip re-index on match. `--full` bypasses.
- **Per-file autocommit, not one giant transaction.** Partial-crash resume is the intended behavior — do not batch writes.
- **Trivial files (<600 B) get persisted with empty summary.** Skipping the row means every run re-considers them.
- **`_LLM_SEMAPHORE` bug watch.** Mira had a latent serial-await bug: naively porting `for _, batch := range batches { g.Go(...); g.Wait() }` recreates it. Fire all batches into one `errgroup`, drain at the end.
- **JSON parse tolerance.** LLMs emit unescaped backslashes (Windows paths, `\App\Models`) and control chars. Two-pass repair: try `json.Decoder`; on `SyntaxError` mentioning "invalid character" run the escape-fixer walk. This is the difference between "one bad response nukes a batch" and "one bad response is silently recovered."
- **`code_comment` line resolution is server-side.** Never trust the model's `start_line`/`end_line`; always re-derive from `existing_code` via the line-snapper.
- **`WithMaxRetries(5)` compounds.** Each LLM call can become 6 HTTPS requests. Budget with this in mind.
- **Session records are written BEFORE the LLM response arrives.** A mid-request crash leaves an orphan `llm_request` record. Resume ignores it. Preserve the ordering when moving session to any backing store.
- **Anthropic thinking blocks are signed by the provider.** Never parse or reorder `NativeTurn.Payload` outside its originating adapter. Dropping a thinking block causes 400s on the next request.
- **The overlap sub-pipeline must NEVER block the main review.** All errors → `[]` findings. Empty candidate list → short-circuit before any LLM call.
- **`get_learned_rules_text` caps at 10.** Without the cap, a noisy learning loop can drown the prompt. Our YAML rules layer has no learning loop, so this is moot — but the injected block should still cap at ~15 rules to protect the prompt.
- **Stacked-branch suppression is by name, not history.** `head_ref/base_ref` string compare. Cheap and works for the common case; a graph-based check is overkill.
- **`path_pattern` is dead metadata in Mira.** Stored, editable, never consulted at injection. Wiring real glob scope during our port is a strict upgrade, not a regression.
- **`content_hash` uses UTF-8 with `errors="replace"`.** Go equivalent: replace invalid UTF-8 with U+FFFD before hashing to stay stable across corrupt bytes.
- **Manifest indexing runs `clear_manifest_packages_for_missing_files(live)` even on empty result.** Do not short-circuit early — otherwise stale manifest rows persist after a manifest file is deleted.
- **Go import resolution needs a state machine, not a regex.** Go source has quoted strings everywhere; a regex over the whole file will destroy JIT context quality.

---

## 8. Ordering for Phases 3–8

Given the dependency graph, the sensible sequence is:

1. **Phase 3** — `pathutil` + `gitcmd` + `diff/*` + `model/{Diff, LlmComment}`. All from OCR. All copy-with-attribution. No LLM required to run their tests. This gives us a working diff parser + line-snapper in ~2 days.
2. **Phase 3 cont.** — `internal/llm/*` + `internal/tool/*` (minus `code_comment` positioning). Provider layer + tool schemas. Also OCR copy.
3. **Phase 4** — `internal/index/*` + `internal/context/*` + `internal/manifests/*` + `internal/extract/*` + `internal/filetype`, `internal/filter`, `internal/chunker`. All from Mira, all port. This is the biggest phase — 3-4 days.
4. **Phase 5** — `internal/overlap/*` + `internal/gh`. From Mira. 1 day.
5. **Phase 6** — `internal/select` + `internal/comment/*` (positioning wired to `diff/resolver.go`). From OCR. 2 days.
6. **Phase 7** — `internal/rules/*`. YAML loader, glob selector, prompt injection. Some new code, some Mira semantics. 1 day.
7. **Phase 8** — Wire together in `cmd/zreview/review_cmd.go` + `internal/llmloop/loop.go`. Session persistence. `--resume`. 2 days.

Total: **10–14 days of coding**, then Phase 9 (prod hardening) and
Phase 10 (docs) — see the full plan in the conversation that authored
this repo (or the `# Plan` section of `ROADMAP.md`).
