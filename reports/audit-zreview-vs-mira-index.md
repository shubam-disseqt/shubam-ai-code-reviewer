# sacr vs MIRA — index subsystem audit

**Answer up top:** sacr's index does **not** work the same as MIRA under the hood.

- **Read side (context builder / JIT / imports)** — near-faithful port. Same algorithms, same 60/30/10 budget, same import regexes for Python/JS/TS/Ruby/Java/Go. sacr *adds* Rust and C/C++ JIT resolvers MIRA lacks. Only cosmetic diffs: section-header casing and system-vs-user message placement.
- **Write side (indexer pipeline)** — **partial port with three whole phases silently dropped.** File-tree walk → filter → LLM batch summarize is intact. Manifest scan, conventions scan, and directory summarization all have **schema + interface + parser code but no caller wiring them into `IndexRepo()`**. Their storage rows are never populated in production.

Every claim below cites source. Docs (`.md` in `reports/`, `docs/PORTING.md`, README) are not evidence.

---

## 1. Read side — near-faithful port

### 1.1. Identical algorithms (both sides)

| Behavior | MIRA | sacr | Same? |
|---|---|---|---|
| Tiered budget split (source/summary/dir) | `src/mira/index/context.py:87-94` — 60/30/10 | `internal/reviewctx/indexed.go:40-44` — 60/30/10 | ✓ Identical |
| Token→char heuristic (no tokenizer) | `context.py:20-21` — `_CHARS_PER_TOKEN = 4` | `internal/reviewctx/types.go:25-32` — `charsPerToken = 4` | ✓ Identical |
| Default token budget | 8,000 | 8,000 | ✓ Identical |
| JIT max files fetched | `jit_context.py:99-105` — 8 | `internal/reviewctx/types.go:25-30` — 8 | ✓ Identical |
| JIT per-file char cap | 1,500 | 1,500 | ✓ Identical |
| Fall-through: index → JIT | `engine.py:1141-1142` (`not index_has_data_for_changed`) | `internal/reviewctx/build.go:17-29` (empty output → BuildJIT) | ✓ Same effect |
| File-excerpt truncation | `_trim_symbol`, `context.py:371-380` | `trimExcerpt`, `internal/reviewctx/jit.go:139-151` | ✓ Same logic (only the truncation comment differs: `#` vs `//`) |

### 1.2. Import extraction — regex/heuristics equal for MIRA's 5 languages, plus 2 new in sacr

| Language | MIRA | sacr | Delta |
|---|---|---|---|
| Python | `jit_context.py:28-30`, candidates 147-158 | `internal/reviewctx/imports.go:26`, candidates 144-160 | Identical |
| JS/TS | `jit_context.py:33-35`, candidates 181-194 | `imports.go:30`, candidates 164-181 | Identical |
| Ruby | `jit_context.py:37-40` | `imports.go:31` | Identical |
| Java | `jit_context.py:45-48`, candidates 205-233 | `imports.go:32`, candidates 198-236 | Identical (both cap at 1 candidate) |
| Go | `jit_context.py:108-140` state machine + candidates 259-318 | `imports.go:361-391` + candidates 271-354 | Identical (same `goStdlibHints` list, same tail-match fallback) |
| **Rust** | — | `imports.go:39, 397-452` (`rustUseRe` + `candidatesRust`) | **sacr only** |
| **C/C++** | — | `imports.go:42, 468-497` (`cxxIncludeRe` + `candidatesCXX`) | **sacr only** |

### 1.3. Two purely cosmetic differences (no semantic impact)

- **Section headers**: MIRA uses Title Case (`### Repository Structure`, `### Source Code`, `### Related Files (imported by changed files)`); sacr uses lowercase/renamed (`### Directory context`, `### Source excerpts`, `**Related files ...**` as subheading). Markdown structure preserved.
- **Injection point**: MIRA appends `code_context` to the **user** message (`src/mira/core/engine.py:1109-1155` → `llm/prompts/review.py:102-103`); sacr concatenates onto the **system** message (`cmd/sacr/prompts.go:47-49`). Same content, different slot. Affects prompt-cache reuse strategy, not the LLM's visible context.

### 1.4. One minor sacr-only defensive addition

`internal/reviewctx/indexed.go:379-428` — `dedupeBlastRadius()` collapses repeated Store rows into shallowest-depth entries. MIRA relies on Store returning clean rows. Only affects noisy Store output; no visible behavior change on clean data.

### 1.5. Read-side verdict

**LLM sees the same content on both sides** for identical PRs, with two extra languages resolvable in sacr's JIT fallback. Nothing dropped.

---

## 2. Write side — partial port, three phases dropped

### 2.1. What's ported and intact

`internal/index/indexer.go:69-258` (`IndexRepo`) executes the first four MIRA phases faithfully:

| Phase | MIRA | sacr | Same? |
|---|---|---|---|
| File tree walk | `indexer.py:382-383` (`fetcher.repo_tree()`) | `indexer.go:78` (`walkRepoTree`) | ✓ Same (local disk only in sacr — no tarball option) |
| Filter + trivial split | `indexer.py:383-447` (`_should_index`) | `indexer.go:83-88` (`shouldIndex`) | ✓ Same 600-byte trivial threshold |
| Trivial persist | `indexer.py:457-470` | `indexer.go:148-165` | ✓ Same placeholder LLM output |
| Batch summarize (LLM) | `indexer.py:472-512`, batches from `_build_batches` | `indexer.go:174-244`, batches from `buildBatches` | ✓ Same first-fit-decreasing, 3-file batches, 5000-byte solo threshold |
| Storage schema (7 write-scope tables) | `store.py:19-207` | `internal/index/schema.go:15-81` | ✓ Same tables, same columns |
| Storage upsert semantics | `store.py:480-536` (delete-then-reinsert children) | `internal/index/sqlite_store.go` | ✓ Same pattern |
| Summarize prompt | `llm/prompts/templates/summarize.jinja2` (57 lines) | `internal/prompts/summarize.md` (56 lines) | ✓ Byte-equivalent instructions + JSON schema |
| JSON-repair for LLM output | `indexer.py:210-237` (`strip_think_blocks` + `_strip_code_fences` + lone-backslash repair) | `internal/index/summarize.go:152-165` + `escapeLoneBackslashes` at 225-263 | ✓ Same three-step repair pipeline |
| Concurrency: LLM sem 8, fetch sem 10 | `indexer.py:398, 479` | `indexer.go:180, 270` (`errgroup.SetLimit`) | ✓ Same numbers |

The core file-summarization pipeline is a faithful Go port. Batching, retries (none in either), JSON parsing, storage — all match.

### 2.2. Three phases silently dropped

Grep-verified: neither `IndexRepo` nor any caller under `cmd/sacr/` or `internal/index/` invokes the three missing passes. Their write-side interface methods have **zero production callers**.

**A. Manifest pass — package dependency scan**
- MIRA: `_index_manifests()` called at `src/mira/index/indexer.py:527-538`. Parses `package.json`, `requirements.txt`, `pyproject.toml`, `go.mod`, `Dockerfile`, `composer.json`; writes rows into `package_manifests`.
- sacr: parsers exist under `internal/manifests/` (~1,633 LOC), storage exists (`schema.go:67` table, `sqlite_store.go:590-627` `UpsertManifestPackages`). But:
  ```
  grep -rn "UpsertManifestPackages" cmd/sacr/ internal/ | grep -v _test.go
  → 0 production callers
  ```
  Only the interface declaration + the impl itself. **Nothing populates `package_manifests` on the write path.**

**B. Conventions pass — AGENTS.md / CONTRIBUTING.md scanning**
- MIRA: `_index_conventions()` called at `indexer.py:544-547`. Loads `AGENTS.md`, `CONTRIBUTING.md`, `CONVENTIONS.md`, `STYLE.md`, `.github/copilot-instructions.md`, `.cursorrules`; strips boilerplate; persists into dashboard DB (`indexer.py:603` `set_repo_conventions`).
- sacr: `conventions.Load()` exists in `internal/conventions/conventions.go:59` (adds CLAUDE.md and `.cursor/rules/*.md` support MIRA lacks). But grep for callers under `cmd/sacr/` and `internal/index/`:
  ```
  grep -rn "conventions\.Load" cmd/sacr/ internal/index/ | grep -v _test.go
  → 0 production callers
  ```
  **The extra-capability additions (CLAUDE.md, .cursor/rules/) never fire during indexing.**

**C. Directory summarization — LLM-batched per-directory summaries**
- MIRA: `_summarize_directories()` at `indexer.py:560-561`. LLM-batched in 15-directory groups; writes rows to `directory_summaries` for later retrieval by the read side.
- sacr: `DirectorySummary` type declared in `internal/index/types.go:57-58`, interface method `UpsertDirectorySummary` at `store.go:29`, SQLite impl at `sqlite_store.go:572-573`. Grep for callers:
  ```
  grep -rn "UpsertDirectorySummary" cmd/sacr/ internal/ | grep -v _test.go | grep -v sqlite_store.go
  → 0 production callers
  ```
  **`directory_summaries` table stays empty.** The read side happily queries it (`indexed.go:207` calls `ListDirectorySummaries` for the 10% dir budget), so requests always return no rows — the entire dir-summary slot in the LLM's context stays empty on sacr.

### 2.3. One MIRA-only extra (out of spec)

- **Vulnerability scan** (`indexer.py:549-558`): fire-and-forget async OSV.dev poll. Not in the porting spec. Correctly absent from sacr.

### 2.4. Deferred by spec

- **Postgres backend** (`src/mira/index/pg_store.py`, 1,750 LOC) — sacr explicitly rejects `postgres://` at `internal/index/store.go:54`: `"index: postgres backend not implemented in this phase"`. Documented, not silent.
- **Cross-repo relationships** (`src/mira/index/relationships.py`, 765 LOC) — MIRA-only org-level clustering. Correctly out of scope; sacr has no analog.

### 2.5. What this means for reviews in practice

The 10% "directory context" tier in the LLM's prompt is populated by `ListDirectorySummaries` calls that hit an empty table. That slot silently degrades to blank on every sacr run today. The read side's algorithm still asks for it — the write side just never fills it.

Similarly, no code path can flag "this PR bumps a vulnerable dependency" via the manifest table, because the table has no rows. Vuln awareness in sacr depends entirely on the govulncheck scanner running deterministically (`internal/scanner/govulncheck.go`), not on the index.

---

## 3. Summary matrix

| Concern | sacr vs MIRA | Evidence |
|---|---|---|
| Batching + LLM summarize + storage schema | Same | `indexer.py:472-512` vs `indexer.go:174-244`; `store.py:19-207` vs `schema.go:15-81` |
| File-type / filter gates | Same | `file_types.py` vs `internal/filetype/filetype.go` (identical extension map + indexable set) |
| Summarize prompt | Same content | `summarize.jinja2` vs `internal/prompts/summarize.md` |
| JSON parse + repair | Same | `indexer.py:210-237` vs `summarize.go:152-165 + 225-263` |
| Concurrency numbers | Same (sem 8 / sem 10) | `indexer.py:398, 479` vs `indexer.go:180, 270` |
| Read side — tiered budget | Same | `context.py:87-94` vs `indexed.go:40-44` |
| Read side — import resolution | sacr ≥ MIRA (adds Rust + C/C++) | `imports.go:397-452, 468-497` |
| Read side — blast-radius dedup | sacr more defensive | `indexed.go:379-428` |
| **Manifest pass** | **Missing (parsers ported, never called)** | 0 callers of `UpsertManifestPackages` |
| **Conventions pass** | **Missing (loader ported, never called)** | 0 callers of `conventions.Load` in indexer |
| **Directory summarization** | **Missing (schema exists, never populated)** | 0 callers of `UpsertDirectorySummary` |
| Postgres backend | Deferred by spec | `store.go:54` explicit rejection |
| Cross-repo relationships | Out of scope | Correctly absent |
| Vulnerability scan | MIRA-only (async OSV poll) | Not present in sacr indexer |

---

## 4. Verdict

**Are they working the same under the hood?**

- **Read side: yes** — with two bonuses (Rust, C/C++). If MIRA and sacr both saw the same populated index, the LLM would receive the same context block on both.
- **Write side: no** — sacr does file-level summarization only. The three deterministic/LLM passes that give MIRA its whole-repo picture (manifests → what depends on what; conventions → team norms; directory summaries → the 10% dir tier of the read-side prompt) are absent. Their storage rows never get written, so read-side queries against those tables return empty on every review.

**Practical effect:** sacr's index is "file summaries + symbol graph" only. MIRA's index is "file summaries + symbol graph + package deps + conventions + directory summaries." The read side's tiered assembly plan is intact on both, but on sacr two of those tiers (`### Directory context`, and any manifest/vuln-aware review context) draw from empty tables.

**If you want to close the gap** — in priority order by user-visible impact:

1. Wire `UpsertDirectorySummary` from a new `_summarizeDirectories()` phase in `IndexRepo` (this fixes the empty 10% tier the read side already tries to use).
2. Wire `conventions.Load()` at the same phase point where MIRA does (`indexer.py:544` equivalent) and persist somewhere the read side can pull from.
3. Wire manifest parsers into `IndexRepo` and populate `package_manifests` (enables vuln correlation later).
