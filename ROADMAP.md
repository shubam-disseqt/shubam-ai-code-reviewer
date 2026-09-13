# Roadmap

The project is executed in numbered phases, each closing with a
checkpoint before the next starts. The plan below is the source of
truth — issues and milestones track work within a phase, not phase
sequencing itself.

## Current state — Phase 3 + Phase 4a shipped

Fifteen packages under `internal/`, ~100 Go files, full test suite green
(`go test ./... -race`), 62.8% overall coverage.

- **Phase 2** — Skeleton, CLI, docs viewer, governance, CI (initial commit)
- **Phase 3** — OCR diff-precision layer copied with Apache-2.0
  attribution. All packages build and their retained tests pass:
  - `internal/pathutil`, `internal/gitcmd`, `internal/model` — foundation
  - `internal/diff/*` (18 files, incl. OCR's 880-LOC resolver regression harness) — **93.8% coverage**
  - `internal/tool/*` (definitions, file_read/find/search, filereader, tools.json embed) — **93.2% coverage**
  - `internal/comment/*` (parse, args_repair, collector) — **99.5% coverage**
  - `internal/llm/*` — providers trimmed to 5 (Anthropic, OpenAI,
    Bedrock, OpenAI-Responses, DeepSeek); retry-report ledger dropped.
    Coverage 40% because agent dropped tests that depended on removed
    providers — will come back up as we exercise the retained code.
  - `internal/llmloop/*` (loop, pool, compression) — retry-ledger
    dropped; a small local `Session` interface stands in until
    `internal/session` lands. Coverage 22% for the same reason as `llm`.
  - `internal/prompts/*.md` — main_task + memory_compression templates
    embedded
- **Phase 4a** — Independent Mira leaf packages ported Python → Go,
  stdlib-only, all with 91–100% coverage:
  - `internal/filetype/` (100%), `internal/filter/` (94.6%),
    `internal/chunker/` (100%), `internal/conventions/` (91.9%)

**What is not yet wired.** Packages exist and each test suite passes,
but end-to-end orchestration (`zreview review` producing real output)
needs Phase 8's wire-up. The `Session` interface, the index store,
overlap, rules, and the review command itself are still ahead.

## Phase 4 continued — Mira index core (planned)

Biggest single remaining chunk. Python → Go.

- `internal/index/{indexer,batch,summarize,store,sqlite_store,postgres_store,schema,types,status}.go`
- `internal/context/{jit,review,imports,hunk}.go`
- `internal/manifests/*.go` — go.mod, package.json, pyproject.toml, Dockerfile, composer.json, lockfiles
- `internal/extract/extract.go`

Deliverable: `zreview index --repo .` produces an index in a local
SQLite file or an external Postgres. `zreview index-inspect <path>`
prints the summary + symbols + imports for one file. JIT context
mode works on a repo with no index.

## Phase 5 — Mira overlap detector (planned)

- `internal/overlap/{overlap,prefilter,prompt,parse,render}.go`
- `internal/gh/{pulls,files,client}.go` via `google/go-github/v63`
- Fingerprint persistence (reuse the index Store — same DB)

Deliverable: `zreview overlap --pr 42` prints candidate overlapping PRs
with kind + confidence + shared files.

## Phase 6 — Precision wiring (planned)

- `internal/select/selection.go` — deterministic file selection ported from OCR agent
- Line-snap positioning wired end-to-end (LLM comment → hunk match → snap)
- Reflection / dedup pass
- Bundling deferred (one-file-per-review dispatch in v1)

Deliverable: `zreview review --from main --to HEAD` produces
line-accurate review comments on a real diff.

## Phase 7 — Org rules layer (planned)

- `internal/rules/{loader,schema,selector,inject,glob}.go`
- Shallow `git clone` of `$ZREVIEW_ORG_RULES_REPO` at review time
- YAML → filter by `scope` (global | repo | path) → inject into prompt

Deliverable: a repository of `.yaml` rules pulled at review time,
scoped by glob, injected into the review prompt under
`## Custom Review Rules`.

## Phase 8 — Review engine wire-up (planned)

- Full `cmd/zreview/review_cmd.go` orchestration
- Session JSONL append log + `--resume`
- `--format stdout | json | github` outputs
- GitHub PR comment poster via `internal/gh`

Deliverable: a real end-to-end review on a real PR, `--format github`
posts inline comments.

## Phase 9 — Production hardening (planned)

- `goreleaser`-free release: matrix build + SHA-256 sums + SLSA build-provenance attestation
- `install.sh` (POSIX) + `install.ps1` (PowerShell) + `action.yml` (GitHub Action)
- npm publishing via platform-stub packages + `bin/zreview.js` launcher
- Docker image
- `zreview doctor` — pre-flight for creds, DB reachability, provider API
- `govulncheck` in CI
- Threat-model doc finalized

## Phase 10 — Docs and launch prep (planned)

- Full Astro Starlight site under `docs/` — 14 pages (see the plan
  that authored this repo)
- `zreview docs` serves the built site from embed.FS
- GitHub Pages deploy for the online copy
- CHANGELOG.md initial cut
- v0.1.0 tagged

## Phase 11 — Pilot on a disseqt repo (planned)

- Wire against one production repo (default: `disseqt-auth-service`)
- 1 week of real PRs, tune noise/precision thresholds
- Bug-fix release cadence
- After the pilot: v1.0.0

## Not planned

- Dashboard, web UI, webhook server
- Learning loop / feedback synthesis
- Vulnerability scanning / SCA / SAST
- Delegation mode, MCP server, plugin marketplace
- Fine-tuning or custom-hosted models
- Cross-repo dependency graph
- Multi-language docs (English only for launch)
- VSCode / JetBrains editor extension
