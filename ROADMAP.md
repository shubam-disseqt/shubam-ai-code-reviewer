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
  `sacr index --repo .` produces an index in SQLite or Postgres.
- **Phase 5** (Mira overlap) — `internal/overlap/*` (96.4%),
  `internal/gh/*` (87.8%). `sacr overlap` surfaces candidate
  overlapping PRs.
- **Phase 6** (Precision wiring) — `internal/selector/*` (100%),
  `internal/reviewctx/*` (89.4%). Line-snap + dedup wired end-to-end.
- **Phase 7** (Org rules) — `internal/rules/*` (93.7%). YAML rules
  pulled at review time, scoped by glob, injected into the prompt.
- **Phase 8** (Review engine wire-up) — `cmd/sacr/review_cmd.go`
  orchestrates the full pipeline; `internal/session/*` (91.8%) writes
  the JSONL append log; `--format stdout | json | github` supported;
  GitHub PR comment poster via `internal/gh`.
- **Phase 9 (partial)** — matrix release with SHA-256 sums and SLSA
  build-provenance attestation, `scripts/install.sh` / `scripts/install.ps1`,
  `action.yml`, Dockerfile, and `sacr doctor` are in place.

## Phase 9 — Production hardening (remaining)

- [x] npm publishing via platform-stub packages + `bin/sacr.js` launcher
  (see `npm/` and the `npm-publish` job in `.github/workflows/release.yml`)
- [x] `govulncheck` in CI (`.github/workflows/govulncheck.yml`, plus a
  local `make vuln` target)
- [x] Threat-model doc finalized (`docs/THREAT_MODEL.md`)
- [x] `llm` / `llmloop` coverage lifted back above 80% (89.3% / 88.5%)

## Phase 10 — Docs and launch prep (in progress)

- **Done** — Full docs site under `docs/`, 14 hand-written HTML pages
  (index, architecture, quickstart, installation, configuration,
  cli-reference, providers, index-and-context, review-rules, overlap,
  session-log, github-action, troubleshooting, security). Switched
  from the originally-planned Astro Starlight to plain static HTML +
  CSS so the same files can be embedded via `//go:embed` with no
  build step — one source of truth for the offline viewer and the
  online copy.
- **Done** — `sacr docs` serves the built site from `embed.FS`
  (loopback-only, strict CSP, host allowlist).
- **Done** — GitHub Pages deploy for the online copy — see
  `.github/workflows/pages.yml`. Triggers on push to `main` when
  `docs/**` changes, plus manual dispatch. Serves `docs/` as-is.
- **Pending** — v0.1.0 tagged (release step; `release.yml` generates
  release notes from `git log` on tag push).

## Phase 11 — Pilot on a disseqt repo (ready to run)

Default target: `disseqt-auth-service`. Drop the workflow snippet from
[docs/github-action.html](docs/github-action.html) into the target
repo, set the provider API key as a repo secret, and run for one week.
Track false-positive rate, line-precision hit rate, and median cost
per review; cut a `v0.1.x` patch on any confirmed regression. Move to
v1.0.0 after five consecutive days at <20% false positives and ≥95%
line-precision. Everything else about the pilot is execution, not
code — this repo ships v0.1.0 with the pilot ready to
kick off.

## Phase 12 — Model tiering (shipped)

Two-tier LLM client resolution: `Main` (Sonnet-class, reviewer) and
`Cheap` (Haiku / Flash / DeepSeek — summary, labeling). Env vars
`SACR_CHEAP_MODEL` + `SACR_CHEAP_PROVIDER` opt in; cheap falls
back to Main when unset. Foundational for Phases 15 and 17.

Deliverable: `internal/llm/tiers.go` + `Tiers{Main, Cheap}` consumed
by `cmd/sacr/review_cmd.go`. Measured target: 30-40% cost cut on
reviews that add PR summary + labels (Ellipsis benchmark).

## Phase 13 — Fingerprinting + incremental re-review (shipped)

Persistent per-PR findings under `~/.sacr/findings/<owner>_<repo>_<pr>.json`.
Stable fingerprint = `sha256(owner|repo|category|normalized_path|symbol|normalized_snippet)`,
whitespace-collapsed and comment-stripped so pure formatting doesn't
reset state. On re-review: findings whose file is untouched
carry-over as `unchanged → keep`; matching fresh finding →
`resolved → drop`; files touched with nothing found → `fixed → resolve`.

Deliverable: `internal/fingerprint/*` + `internal/findings/*` +
integration in `cmd/sacr/review_cmd.go`. Measured target: 40-50%
token cut on iterative PR pushes (Ellipsis benchmark).

## Phase 14 — Deterministic security scanners (shipped)

Gitleaks (secrets), Semgrep (SAST), and govulncheck (Go stdlib CVE)
run concurrently over `kept` files via `errgroup`; JSON output parsed
into `ScannerFinding{RuleID, Path, Line, Kind, Severity, ...}`.
Findings flow into the same collector as LLM findings tagged
`Source=scanner:<tool>`. Best-effort: a missing tool binary skips
with an info line — never fatal.

Deliverable: `internal/scanner/{gitleaks,semgrep,govulncheck,runner}.go`
plus a new `## Known Issues (from static analysis)` block in the
system prompt so the LLM enriches rather than restates. Docs page:
`docs/scanners.html`.

## Phase 15 — Summarizer + Labeler cheap-model agents (shipped)

Two structured-JSON calls on the cheap tier, run parallel with
scanners in an `errgroup`. Summarizer produces
`{walkthrough, change_groups, testing_notes, risk}`; Labeler produces
`{pr_type, domains, risk_tag, ownership_hints}`. Both best-effort —
empty on failure.

Deliverable: `internal/prompts/{summarizer,labeler}.md` +
`cmd/sacr/{summary,label}.go`. Consumed by Phase 17's PR
description block and by GitHub labels.

## Phase 16 — Scoring engine (shipped)

Deterministic policy: `Score(finding) → {Severity, Confidence, Impact}`,
table-driven from `internal/scoring/policy.yaml` (embedded default,
overridable via `SACR_SCORING_POLICY` or `.sacr/scoring.yaml`).
`emit.go`'s `filterResolved` becomes `filterByScore`; `--min-severity`
flag gates the publish stream (default `MEDIUM`).

Deliverable: `internal/scoring/*`. Explicitly not an LLM — CR-bench
data ruled out reflection loops as a substitute.

## Phase 17 — SARIF output + PR description block (shipped)

- `internal/sarif/` — SARIF 2.1.0 encoder with golden-file tests
  against the schema. `--format sarif` emits to file or stdout.
- `internal/gh/` gains `GetPRBody`, `UpdatePRBody`, `AddLabels`,
  `UploadSARIF`. Feature-gate initial SARIF upload behind
  `SACR_UPLOAD_SARIF=1`.
- `cmd/sacr/description.go` — idempotent PR body update between
  `<!-- SACR:BEGIN -->` and `<!-- SACR:END -->` markers with
  the Phase 15 walkthrough + Phase 16 severity summary.

Deliverable: security findings flow to GitHub Code Scanning (SARIF);
review comments stay on the diff; PR description carries the
sacr-managed walkthrough block.

## Explicitly excluded (from the LangGraph-Edition design)

Documented so scope creep is loud. See
[docs/ARCHITECTURE.md §9](docs/ARCHITECTURE.md#9-non-goals) for the
non-goals section that owns these.

- **LangGraph orchestration framework.** `llmloop` stays.
- **Reflection Agent / Adjudicator LLM loop.** CR-bench (NUS, 2026)
  measured lower usefulness than single-shot.
- **Best Practices Agent as separate node.** Rules layer already
  covers it.
- **Manual effort estimator.** Feature bloat until pilot data
  justifies it.
- **Python service split.** Single Go binary is the distribution
  contract.

## Not planned

- Dashboard, web UI, webhook server
- Learning loop / feedback synthesis
- Deep SCA / dependency-tree / license analysis (Phase 14 is scoped
  to secrets + lightweight SAST + Go stdlib CVE)
- Delegation mode, MCP server, plugin marketplace
- Fine-tuning or custom-hosted models
- Cross-repo dependency graph
- Multi-language docs (English only for launch)
- VSCode / JetBrains editor extension
