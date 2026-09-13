# Architecture

This document is the source of truth for how `z-code-reviewer` is built.
It is authored to match the shape of `alibaba/open-code-review`'s
[architecture doc](https://github.com/alibaba/open-code-review) and
[`ASSURANCE_CASE.md`](https://github.com/alibaba/open-code-review/blob/main/ASSURANCE_CASE.md):
Mermaid for the data-flow pipeline, ASCII for trust boundaries.

The same content is embedded in the binary — run `zreview docs` to view
it offline.

---

## 1. What we build, in one paragraph

`zreview` is a Go CLI. It takes a git diff (or a range, or a commit),
optionally consults a **persistent code index** for repo-wide context,
optionally consults an **org-level rules repo** for team-specific review
policy, calls an LLM through a **thin agent loop** with a small set of
tools (file read, code search, file find, comment), snaps LLM-produced
comments to real diff lines via a deterministic post-processor, and
optionally scans other open PRs for overlap or merge-conflict risk. The
output is a stream of comments — printed to stdout, emitted as JSON, or
posted to a PR via a `--format github` writer.

There is no long-running server, no dashboard, no webhook receiver, and
no persistent review history. The index and the rules are the only
things that live longer than a single invocation.

---

## 2. Pipeline

```mermaid
flowchart TD
    A[git diff / range / commit] --> B[deterministic<br/>file selection]
    B --> C[bundle<br/>related files]
    C --> D[load index context<br/>+ JIT if index empty]
    D --> E[load matching<br/>org rules]
    E --> F[LLM agent loop<br/>tools: file_read, code_search,<br/>file_find, code_comment,<br/>task_done]
    F --> G[comment collector]
    G --> H[line-snap<br/>positioning]
    H --> I[reflection<br/>dedup, low-confidence filter]
    I --> J[format<br/>stdout &#124; json &#124; github]

    K[open PRs list] -.-> L[fingerprint<br/>prefilter Jaccard]
    L -.-> M[LLM overlap<br/>verdict]
    M -.-> J
```

Dashed edges are the cross-PR overlap sub-pipeline: it runs in parallel
with the main review and never blocks it — every failure returns "no
overlap findings" rather than failing the run.

---

## 3. Package map

Not yet all built. Each row lists the target package, its job, and the
source of the approach.

| Package | Job | Source |
|---|---|---|
| `cmd/zreview` | Cobra CLI: `review`, `scan`, `docs`, `version`, `config`, `llm`, `rules` | OCR shape |
| `internal/diff` | Unified-diff parser, hunk resolution, gitignore, workspace file guard | OCR — copy verbatim |
| `internal/gitcmd` | Bounded-concurrency git subprocess runner, `--end-of-options` safe | OCR — copy verbatim |
| `internal/pathutil` | `CanonicalPath`, `WithinBase` (symlink + escape guards) | OCR — copy verbatim |
| `internal/select` | Deterministic file selection (binary/ext/size gates, no silent skips) | OCR `internal/agent/selection.go` |
| `internal/bundle` | Related-file bundling into isolated review units | OCR `internal/agent/grouping.go` (v2 — v1 is one-file-per-review) |
| `internal/comment` | Comment parsing, `existing_code` line snapping, arg repair, reflection/dedup | OCR `tool/{code_comment,comment_args_repair,comment_collector}` + `diff/resolver.go` |
| `internal/llm` | Provider abstraction — Anthropic / OpenAI / Bedrock / Gemini / DeepSeek | OCR `internal/llm/*` — copy, trim providers |
| `internal/llmloop` | Thin agent loop, compression, comment worker pool | OCR `internal/llmloop/*` — copy, drop retry-report ledger |
| `internal/tool` | Tool schema + `file_read`, `code_search`, `file_find`, `code_comment`, `task_done` | OCR `internal/tool/*` — copy verbatim |
| `internal/index` | Repo index writer: file summaries + symbols + imports + external refs | Mira `src/mira/index/*` — port Python → Go |
| `internal/index/store` | Store interface + SQLite (`modernc.org/sqlite`) + Postgres (`pgx/v5`) backends | Mira `store.py` + `pg_store.py` — port + trim to indexing tables |
| `internal/context` | JIT context (index-miss path) + review-time context join (index-hit path) | Mira `jit_context.py` + `core/context.py` — port |
| `internal/manifests` | Deterministic package-manifest parsers: `go.mod`, `package.json`, `pyproject.toml`, `Dockerfile`, `composer.json`, lockfiles | Mira `manifests.py` — port, use Go-native libs where they exist (`x/mod/modfile`) |
| `internal/extract` | Language-aware symbol/import extractors (regex + brace/indent walkers) | Mira `extract.py` — port |
| `internal/conventions` | Pull `AGENTS.md` / `CONTRIBUTING.md` etc., strip boilerplate, cap 8K chars | Mira `conventions.py` — port |
| `internal/rules` | Git-cloned org rules repo → YAML load → glob-scoped filter → prompt injection | Adapted from Mira `learned_rules` + `review_context`; **YAML-in-git replaces DB** |
| `internal/overlap` | Cross-PR fingerprinting, Jaccard prefilter, batched LLM verdict, tolerant JSON parse | Mira `core/overlap.py` — port |
| `internal/gh` | GitHub REST via `google/go-github`: list open PRs, get PR files, post review comments | Mira `providers/github.py` — port shape |
| `internal/session` | JSONL append log for `--resume`, chained by `parentUuid` | OCR `internal/session/*` — copy, drop viewer, drop manifest coverage sets (v2) |
| `internal/docs` | Embedded static docs + local HTTP server for `zreview docs` | New — modeled on OCR `internal/viewer/{server,hostguard,securityheaders}.go` |
| `internal/prompts` | Embedded prompt templates (`main_task_system.md`, `main_task_user.md`, `memory_compression_task.md`, `summarize.md`, `overlap.md`) | OCR + Mira — copy verbatim |

The per-file map, LOC estimates, and modifications needed live in
[PORTING.md](PORTING.md).

---

## 4. Deterministic vs LLM boundary

`zreview` is not a pure agent. It is a rigid Go pipeline with an LLM
loop wedged in the middle. Determinism sits on both ends because that
is where the model's failure modes live.

| Step | Owner | Why |
|---|---|---|
| Which files to review | Go | Coverage guaranteed — no "agent skipped 3 files" |
| How to bundle files | Go | Isolated sub-agent contexts; parallelizable; stable on large diffs |
| Which rules match a file | Go (glob against YAML) | More predictable than prompt-injected rules |
| Reading a file, searching code | LLM via tools | Dynamic context is where agents earn their keep |
| Writing comments | LLM | Only creative task |
| Line-number positioning | Go post-processor (line-snap against real hunks) | Fixes the classic LLM off-by-N bug |
| Comment reflection / dedup | Go | Filters low-quality comments before they hit the user |
| Cross-PR overlap prefilter | Go (Jaccard on title, symbol/file intersect) | Cheap gate before any LLM call |
| Cross-PR overlap verdict | LLM (batched) | Pattern-match on intent only |

---

## 5. Trust boundaries and threats

The threat surface is intentionally small. This is a per-invocation
CLI — no persistent process, no webhook endpoint, no bound port
except when the user explicitly runs `zreview docs`.

```
┌───────────────────────────────────────────────────────────────┐
│  User machine or CI runner (Trusted Zone)                     │
│                                                               │
│  ┌──────────┐    ┌──────────────┐    ┌────────────────────┐   │
│  │ Git repo │───▶│  zreview CLI │───▶│ Local output       │   │
│  │ (diffs)  │    │  (core)      │    │  stdout / json /   │   │
│  └──────────┘    └──┬──────┬────┘    │  session .jsonl    │   │
│                     │      │         └────────────────────┘   │
│                     │      │                                  │
│                     │      └──────▶ ┌────────────────────┐    │
│                     │               │  Docs server       │    │
│                     │               │  (opt-in, loop     │    │
│                     │               │   back only, host  │    │
│                     │               │   allowlist)       │    │
│                     │               └────────────────────┘    │
└─────────────────────┼──────────┼──────────────────────────────┘
                      │          │
              TLS ────┘          └──── TLS
                      │          │
                      ▼          ▼
        ┌─────────────────┐   ┌──────────────────┐   ┌──────────────────┐
        │ LLM provider    │   │ Index database   │   │ Org-rules repo   │
        │ (HTTPS)         │   │ (Postgres/SQLite)│   │ (git, HTTPS/SSH) │
        └─────────────────┘   └──────────────────┘   └──────────────────┘
```

### Actors

| Actor | Trust level |
|---|---|
| Local user / CI runner | Trusted — invokes the CLI with full control over configuration |
| LLM provider API | Semi-trusted — responses validated before use, line numbers re-derived server-side |
| Git repository content | Semi-trusted — diffs may contain adversarial content |
| Index database | Semi-trusted — user-owned, but stores LLM-summarized content that may be adversarially crafted |
| Org-rules repository | Trusted — protected by same GitHub access controls as source |
| Network | Untrusted — all outbound traffic uses TLS |
| Web browser (docs viewer) | Untrusted — may attempt DNS rebinding against the local docs server |

### Threat summary

| ID | Threat | Mitigation |
|---|---|---|
| T1 | Command injection via crafted diff content | External process execution restricted to `git`, hardcoded subcommands, `--end-of-options`, no shell interpolation (ported from OCR `internal/gitcmd`) |
| T2 | API key leakage | Keys read from environment variables only; never logged, never written to session JSONL, never transmitted beyond the configured endpoint |
| T3 | Path traversal via LLM-suggested file paths | `internal/pathutil.WithinBase()` validates all file paths against the repository root, pre- and post-symlink resolution (ported from OCR) |
| T4 | DNS rebinding against local `zreview docs` server | Host-header allowlist rejects requests from non-loopback origins; wildcard binds require explicit `ZREVIEW_DOCS_ALLOWED_HOSTS` (ported from OCR `internal/viewer/hostguard.go`) |
| T5 | MITM on API communication | Go's `net/http` enforces TLS 1.2+ with certificate verification by default; `InsecureSkipVerify` is never set anywhere in the codebase |
| T6 | Malicious LLM response (fabricated line numbers, off-diff comments) | JSON schema validation on response structure; line-number bounds checking against actual diff ranges; line-snap positioning fixes off-by-N |
| T7 | Malicious LLM response (over-escaped JSON, prose read as structure) | `internal/comment.CommentArgsRepair` refuses partial recovery; rejects on odd double-quote count and unknown schema fields |
| T8 | Dependency vulnerabilities | `govulncheck` runs in CI on every push; Dependabot monitors upstream; `go.sum` provides integrity verification |
| T9 | PHI / sensitive code sent to third-party LLM | Same posture as the user's other LLM tooling; document in `SECURITY.md`; org rules can flag PHI paths for extra scrutiny |
| T10 | Malicious content in an org rules repo | Rules are text injected into a prompt, not code executed; a malicious rule can bias review output but not achieve RCE on the CLI |

### Secure design principles

| Principle | How applied |
|---|---|
| Least privilege | `CGO_ENABLED=0` build eliminates C-library attack surface. No network listeners except the opt-in docs viewer. |
| Fail-safe defaults | API keys must be explicitly provided. Docs viewer binds to `127.0.0.1` by default; non-loopback binds require explicit allowlisting. |
| Complete mediation | Every path from LLM output is validated against the repo root before disk access. Every docs-server request is checked against the host allowlist. |
| Economy of mechanism | Exactly one external binary is exec'd (`git`), with hardcoded subcommands and no shell. |
| Open design | Apache-2.0. Security relies on TLS, not obscurity. |
| Separation of privilege | API credentials, database credentials, and org-rules-repo access credentials are three distinct env vars; no single leak escalates. |
| Least common mechanism | Each review invocation writes to its own session file. No shared state between concurrent invocations. |
| Psychological acceptability | Security defaults (TLS, localhost, allowlist) require no user config. Overrides (`ZREVIEW_DOCS_ALLOWED_HOSTS`, `ZREVIEW_DB_URL`) are explicit and documented. |

---

## 6. Data flow

What leaves the user's machine and where:

| Leaves | To | Contents | Why |
|---|---|---|---|
| HTTPS | LLM provider (Anthropic, OpenAI, Bedrock, ...) | Diff hunks, file source pulled by tools, prompt templates, rules text | The review itself |
| HTTPS | Index database (Postgres or local SQLite) | File summaries, symbols, imports, external refs — as generated by the indexing model | The index |
| HTTPS/SSH | Org rules repo (GitHub / GitLab / Forgejo) | Read-only shallow clone | To load YAML rules |
| HTTPS | GitHub API | Repo metadata, open PR list, PR files, PR review comment posts | Cross-PR overlap and `--format github` |

Nothing leaves for telemetry unless the operator explicitly sets
`ZREVIEW_TELEMETRY=1`, and even then only anonymous run counters — no
code, no diffs, no comments.

---

## 7. State and persistence

Three durable stores. Only the first is required.

| Store | Required? | What it holds | Rebuildable? |
|---|---|---|---|
| Filesystem session log | Yes (always local) | Append-only JSONL per invocation for `--resume` | Rebuildable — session is rerun-safe |
| Index DB (Postgres or SQLite) | Optional (features degrade to JIT context if absent) | Per-file summaries, symbols, imports, external refs, package manifests | Rebuildable — `zreview index --full` recomputes |
| Org rules repo (git) | Optional (features degrade to no-rules if absent) | YAML files describing review rules with scope + severity + category | Source of truth in git |

No review history is persisted. Comments are ephemeral — they land in
the PR (via `--format github`), or in stdout / JSON, and that is the
end of their lifecycle in `zreview`.

### Index schema (indexing subset only)

```sql
CREATE TABLE files (
    path         TEXT PRIMARY KEY,
    language     TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    loc          INTEGER NOT NULL DEFAULT 0,
    updated_at   REAL NOT NULL DEFAULT 0
);
CREATE TABLE symbols (
    file_path   TEXT NOT NULL,
    name        TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'function',
    signature   TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (file_path, name),
    FOREIGN KEY (file_path) REFERENCES files(path) ON DELETE CASCADE
);
CREATE TABLE imports (
    source_path TEXT NOT NULL,
    target_path TEXT NOT NULL,
    PRIMARY KEY (source_path, target_path),
    FOREIGN KEY (source_path) REFERENCES files(path) ON DELETE CASCADE
);
CREATE TABLE symbol_refs (
    source_path   TEXT NOT NULL,
    source_symbol TEXT NOT NULL,
    target_path   TEXT NOT NULL,
    target_symbol TEXT NOT NULL,
    PRIMARY KEY (source_path, source_symbol, target_path, target_symbol),
    FOREIGN KEY (source_path) REFERENCES files(path) ON DELETE CASCADE
);
CREATE TABLE directories (
    path       TEXT PRIMARY KEY,
    summary    TEXT NOT NULL DEFAULT '',
    file_count INTEGER NOT NULL DEFAULT 0,
    updated_at REAL NOT NULL DEFAULT 0
);
CREATE TABLE external_refs (
    file_path   TEXT NOT NULL,
    kind        TEXT NOT NULL,
    target      TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (file_path, kind, target),
    FOREIGN KEY (file_path) REFERENCES files(path) ON DELETE CASCADE
);
CREATE TABLE package_manifests (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT '',
    version    TEXT NOT NULL DEFAULT '',
    file_path  TEXT NOT NULL DEFAULT '',
    is_dev     INTEGER NOT NULL DEFAULT 0,
    updated_at REAL NOT NULL DEFAULT 0,
    UNIQUE(name, kind, file_path)
);
CREATE INDEX idx_pkg_manifest_name ON package_manifests(name);
```

Postgres mirror prepends `(owner TEXT, repo TEXT)` to every primary key
and uses `TIMESTAMPTZ` instead of the SQLite epoch-float `REAL`
column. The `Store` interface hides the split; callers pass
`(owner, repo)` to `Open` for both backends.

Adapted from Mira's `src/mira/index/store.py`. See [PORTING.md](PORTING.md) §Index.

### Org rules YAML shape

```yaml
# rules/no-console-log.yaml
id: no-console-log
title: No console.log in production code
body: |
  Flag any console.log/warn/error left in application code.
  Test fixtures under tests/** are exempt.
scope: repo                   # global | repo | path
repos: ["disseqt/z-frontend"]
paths: ["src/**/*.ts", "src/**/*.tsx"]
exclude_paths: ["**/*.test.ts"]
severity: warning             # blocker | warning | suggestion | nitpick
category: maintainability
enabled: true
```

Loaded at review time via shallow `git clone` of
`$ZREVIEW_ORG_RULES_REPO`, filtered by `scope` and glob against the
current diff's file list, then rendered into the review prompt under a
`## Custom Review Rules` block. See [PORTING.md](PORTING.md) §Rules.

---

## 8. Extensibility points

Where a future contributor plugs in new capability without touching the
core:

| Extension | Where |
|---|---|
| New LLM provider | Add a `Protocol` constant to `internal/llm/protocol.go` and a client adapter in `internal/llm/`. Register in `internal/llm/providers.go`. |
| New manifest parser | Add a `Parser` implementation in `internal/manifests/` and register in the dispatch table. |
| New symbol-extraction language | Add a regex + walker in `internal/extract/` keyed by language enum. |
| New docs page | Drop a Markdown file into `docs/src/content/docs/en/` and rebuild. `zreview docs` picks up the new page from the embedded FS. |
| New tool the agent can call | Implement `tool.Provider` in `internal/tool/`, register in `Registry` at startup, add its JSON schema to `internal/config/toolsconfig/tools.json`. |
| New output format | Add a formatter under `internal/format/` and register via `--format` flag switch in `cmd/zreview/review_cmd.go`. |

---

## 9. Non-goals

Called out explicitly so scope creep is loud:

- **No dashboard.** No web UI beyond the local docs viewer.
- **No webhook server.** CLI only. CI/CD is the delivery mechanism.
- **No learning loop.** Rules are authored by humans and reviewed via
  git PR on the rules repo.
- **No vulnerability scanning.** OSV / SCA is out of scope — different
  tool, different failure modes. Users layer their own SAST/SCA in CI.
- **No cross-repo dependency graph.** Mira's `relationships.py`
  intentionally not ported.
- **No fine-tuning or custom models.** Wrong tool for the job.
- **No plugin marketplace, delegation mode, MCP server, or agent skill
  packaging.** These may return later as opt-in adapters, not first-class
  features.
