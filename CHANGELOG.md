# Changelog

All notable changes to `z-code-reviewer` are documented in this file.

The format is based on [Keep a Changelog 1.1](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Entries are grouped by change type: **Added**, **Changed**, **Deprecated**,
**Removed**, **Fixed**, **Security**. Every non-trivial change lands here
before it lands in a release; the release tag simply closes an
`[Unreleased]` section and opens a new one.

## [Unreleased]

### Added

- Documentation site under `docs/` served offline by `zreview docs` and
  online via GitHub Pages. Fourteen hand-written HTML pages, no build
  step, no third-party assets — one source of truth for both surfaces.
- `zreview review` end-to-end pipeline: diff resolution, deterministic
  selector, optional index-backed context, org-rules injection, LLM
  loop, line-snapping post-processor, and multi-format output
  (`stdout` | `json` | `github`).
- `zreview index` — SQLite/Postgres persistent code index with
  incremental content-hash caching, per-file summaries, symbol maps,
  conventions, and manifest extraction.
- `zreview overlap` — cross-PR overlap detection with deterministic
  prefilter (title Jaccard + file/symbol intersection) followed by a
  batched LLM verdict pass. Best-effort; never blocks review output.
- `zreview rules list` and `zreview rules sync` — inspect and refresh
  the org rules repo named by `ZREVIEW_ORG_RULES_REPO`.
- `zreview doctor` — pre-flight configuration checks with a
  configuration-error exit code so CI fails fast on misconfig.
- `zreview docs` — bundled offline docs viewer with strict CSP,
  loopback bind, host allowlist for non-loopback binds, and no
  third-party assets.
- Five LLM providers wired end-to-end: Anthropic, OpenAI Chat
  Completions, OpenAI Responses, AWS Bedrock (Anthropic models,
  ambient AWS auth), and DeepSeek.
- Session log — every review run writes an append-only JSONL under
  `~/.zreview/sessions/`. Supports `--resume <session-id>` for
  interrupted runs.
- GitHub Actions composite action (`action.yml`) plus install
  scripts (`install.sh`, `install.ps1`) and a `Dockerfile`.
- Matrix release workflow with SHA-256 checksums and SLSA
  build-provenance attestation.
- `internal/` packages ported and adapted from `alibaba/open-code-review`
  (diff precision, tool loop, prompts, comment parser) and
  `miracodeai/mira` (index, overlap, extract, conventions, filter,
  chunker), each with Apache-2.0 attribution recorded in `PORTING.md`
  and `NOTICE`.
- GitHub Pages workflow deploying the `docs/` directory as the
  online documentation surface. Same content the binary embeds.

### Changed

- LLM provider registry trimmed to the five providers actually
  wired to a client, dropping the wider preset list inherited from
  upstream OCR.
- `internal/llmloop` retry ledger removed; failure handling
  simplified to session-record + retry via `--resume`.

### Security

- Docs server enforces `default-src 'self'; script-src 'none';
  frame-ancestors 'none'`, loopback-only bind by default, and a
  Host-header allowlist gate against DNS rebinding.
- No hardcoded secrets in the repo. Provider credentials read from
  environment variables at runtime; AWS Bedrock uses the ambient
  credential chain.
- `zreview doctor` verifies presence of at least one provider
  credential before the review pipeline is dispatched.

<!--
When cutting a release, replace `[Unreleased]` above with the version
number and date, e.g.:

    ## [0.1.0] - 2026-09-14

…and add a fresh empty `## [Unreleased]` section on top for the next
cycle. Link references live at the bottom of the file.
-->

[Unreleased]: https://github.com/shubam-disseqt/z-code-reviewer/compare/HEAD...HEAD
