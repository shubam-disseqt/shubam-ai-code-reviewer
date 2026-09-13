# Roadmap

The project is executed in numbered phases, each closing with a
checkpoint before the next starts. The plan below is the source of
truth — issues and milestones track work within a phase, not phase
sequencing itself.

## Current state — Phases 2–8 shipped, Phase 9 in progress

Twenty-three packages under `internal/`, full test suite green
(`go test ./... -race`). Coverage: most packages 80–100%; `llm` and
`llmloop` still catching up after the provider trim (tracked in
Phase 9 hardening).

- **Phase 2** — Skeleton, CLI, docs viewer, governance, CI (initial commit)
- **Phase 3** — OCR diff-precision layer copied with Apache-2.0
  attribution. All packages build and their retained tests pass:
  - `internal/pathutil`, `internal/gitcmd`, `internal/model` — foundation
  - `internal/diff/*` (18 files, incl. OCR's 880-LOC resolver regression harness) — **93.8% coverage**
  - `internal/tool/*` (definitions, file_read/find/search, filereader, tools.json embed) — **93.2% coverage**
  - `internal/comment/*` (parse, args_repair, collector) — **99.5% coverage**
  - `internal/llm/*` — providers trimmed to 5 (Anthropic, OpenAI,
    Bedrock, OpenAI-Responses, DeepSeek); retry-report ledger dropped.
    Coverage **89.3%** after Phase 9 test-lift covering the retained
    providers via httptest round-trips plus the resolver strategies.
  - `internal/llmloop/*` (loop, pool, compression) — retry-ledger
    dropped; a small local `Session` interface stands in until
    `internal/session` lands. Coverage **88.5%** after Phase 9 test-lift
    for RunMainTask, compression, and Runner metrics.
  - `internal/prompts/*.md` — main_task + memory_compression templates
    embedded
- **Phase 4a** — Independent Mira leaf packages ported Python → Go,
  stdlib-only, all with 91–100% coverage:
  - `internal/filetype/` (100%), `internal/filter/` (94.6%),
    `internal/chunker/` (100%), `internal/conventions/` (91.9%)
- **Phase 4b** (Mira index core) — `internal/index/*` (83.9%),
  `internal/manifests/*` (90.9%), `internal/extract/*` (93.2%).
  `zreview index --repo .` produces an index in SQLite or Postgres.
- **Phase 5** (Mira overlap) — `internal/overlap/*` (96.4%),
  `internal/gh/*` (87.8%). `zreview overlap` surfaces candidate
  overlapping PRs.
- **Phase 6** (Precision wiring) — `internal/selector/*` (100%),
  `internal/reviewctx/*` (89.4%). Line-snap + dedup wired end-to-end.
- **Phase 7** (Org rules) — `internal/rules/*` (93.7%). YAML rules
  pulled at review time, scoped by glob, injected into the prompt.
- **Phase 8** (Review engine wire-up) — `cmd/zreview/review_cmd.go`
  orchestrates the full pipeline; `internal/session/*` (91.8%) writes
  the JSONL append log; `--format stdout | json | github` supported;
  GitHub PR comment poster via `internal/gh`.
- **Phase 9 (partial)** — matrix release with SHA-256 sums and SLSA
  build-provenance attestation, `install.sh` / `install.ps1`,
  `action.yml`, Dockerfile, and `zreview doctor` are in place.

## Phase 9 — Production hardening (remaining)

- npm publishing via platform-stub packages + `bin/zreview.js` launcher
- `govulncheck` in CI
- Threat-model doc finalized
- ~~`llm` / `llmloop` coverage lifted back above 80%~~ (done — 89.3% / 88.5%)

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
