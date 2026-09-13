# Roadmap

The project is executed in numbered phases, each closing with a
checkpoint before the next starts. The plan below is the source of
truth — issues and milestones track work within a phase, not phase
sequencing itself.

## Current state — Phase 2 (skeleton) shipped

- Repository scaffold, meta files, license, attribution
- `cmd/zreview` Cobra CLI with working `version` and `docs` commands
- Embedded docs viewer (localhost, host-allowlisted, DNS-rebinding safe)
- CI: build + vet + test on every push
- `ARCHITECTURE.md` and `PORTING.md` published — the design contract
  we execute against for the remaining phases

## Phase 3 — OCR diff-precision layer (planned)

Copy-with-attribution from `alibaba/open-code-review`. All Apache-2.0 Go.

- `internal/pathutil`, `internal/gitcmd`, `internal/diff/*`
- `internal/comment/{parse,args_repair,collector}.go`
- `internal/tool/*` — file_read, code_search, file_find, task_done, tool schema JSON
- `internal/llm/*` — trimmed to 5 providers (Anthropic, OpenAI, Bedrock, Gemini, DeepSeek)
- `internal/llmloop/{loop,compression,pool}.go` — retry-report ledger dropped
- `internal/prompts/*.md` — main_task and memory_compression prompts

Deliverable: `zreview diff-parse <file>` prints a parsed unified diff
and its resolved hunks. `internal/diff/resolver_test.go` (OCR's 880-LOC
regression harness) passes.

## Phase 4 — Mira index port (planned)

Python → Go. Biggest phase.

- `internal/index/{indexer,batch,summarize,store,sqlite_store,postgres_store,schema,types,status}.go`
- `internal/context/{jit,review,imports,hunk}.go`
- `internal/manifests/*.go` — go.mod, package.json, pyproject.toml, Dockerfile, composer.json, lockfiles
- `internal/extract/extract.go`
- `internal/conventions/conventions.go`
- `internal/filetype`, `internal/filter`, `internal/chunker`

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
