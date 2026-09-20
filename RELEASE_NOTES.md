# v0.2.0 — first shipping build of `sacr`

`sacr` (short for `shubam-ai-code-reviewer`) is an AI code reviewer that runs against your pull requests. This is the first release under the new brand.

## Install

```bash
# Homebrew
brew install shubam-disseqt/tap/sacr

# npm
npm i -g sacr

# Direct binary
curl -fsSL https://github.com/shubam-disseqt/shubam-ai-code-reviewer/releases/download/v0.2.0/install.sh | sh

# Docker
docker pull ghcr.io/shubam-disseqt/sacr:v0.2.0

# GitHub Action
- uses: shubam-disseqt/shubam-ai-code-reviewer@v0.2.0
  with:
    pr-number: ${{ github.event.pull_request.number }}
    api-key: ${{ secrets.OPENAI_API_KEY }}
    github-token: ${{ secrets.GITHUB_TOKEN }}
```

## Quickstart

```bash
export OPENAI_API_KEY=sk-...
export SACR_MODEL=gpt-4o-mini

# review a workspace diff
sacr review

# review a branch range
sacr review --from main --to feature/x

# review a single commit
sacr review --commit abc123

# post to a GitHub PR
export GITHUB_TOKEN=ghp_...
export GITHUB_REPOSITORY=owner/repo
sacr review --pr 42 --format github
```

## What's in this release

### Review pipeline
- 13-stage deterministic pipeline wrapping a thin LLM tool-loop
- Inline PR comments with severity + category, effort score (0–10), reviewer-effort audit table
- Package-imports diagram parsed from real `import` lines (Go / TypeScript / Python)
- Cross-PR overlap detection with confidence scoring
- Content-hash fingerprints so fix commits drop resolved findings cleanly

### LLM providers
- Anthropic (Claude Sonnet / Opus / Haiku)
- OpenAI (GPT-4o, GPT-4o-mini, o1, o3-mini)
- OpenAI Responses API
- AWS Bedrock (ambient AWS credentials — no API key required)
- DeepSeek (`deepseek-chat`, `deepseek-reasoner`) — great for the cheap tier

### Deterministic scanners
Bundled and wired into the review body as SARIF:
- **gitleaks** — hardcoded secrets, API tokens
- **semgrep** — pattern-based bugs & security; curated rules for JS/Python/Ruby
- **govulncheck** — known CVEs in Go dependencies

### Index & context
- SQLite-backed code index (`sacr index`) — file summaries, symbols, imports, external refs
- Deterministic manifest scan (npm / pip / poetry / uv / go / Dockerfile / composer + lockfiles)
- Conventions loader (AGENTS.md / CONTRIBUTING.md / CLAUDE.md / `.cursor/rules/*.md`)
- Directory-level summaries (batched LLM call, LRU'd)
- JIT cross-file context when the index isn't populated — Python, JS/TS, Go, Ruby, Java, **Rust**, **C/C++**

### Comment positioning
- Unified-diff parser + line-snapping resolver
- Cross-file re-location for comments that describe code in a moved file
- LLM re-locate fallback: when neither deterministic path resolves the comment, the LLM is asked to rewrite `existing_code` and the snap is retried
- JSON-repair for malformed LLM tool arguments (over-escaped strings, control-char injection)

### Output formats
- `stdout` — local diagnosis, ANSI-colored
- `json` — for CI dashboards
- `github` — inline PR comments + managed PR body block + labels
- `sarif` — for GitHub Code Scanning; auto-upload when `SACR_UPLOAD_SARIF=1`

### Configuration
- CLI flags (`--from`, `--to`, `--commit`, `--pr`, `--format`, `--min-severity`, ...)
- Environment variables (`SACR_MODEL`, `SACR_CHEAP_MODEL`, `SACR_PROVIDER`, `SACR_DB_URL`, ...)
- Repo-local YAML under `<repo>/.sacr/` (`config.yaml`, `scoring.yaml`, `effort.yaml`)
- Layered precedence: CLI > env > repo-local YAML > embedded defaults

## Migration from prior internal releases

If you were running the previous internal build (`zreview`):

| Old | New |
|---|---|
| `zreview review …` | `sacr review …` |
| `ZREVIEW_MODEL` | `SACR_MODEL` |
| `ZREVIEW_PROVIDER` | `SACR_PROVIDER` |
| `ZREVIEW_DB_URL` | `SACR_DB_URL` |
| `ZREVIEW_UPLOAD_SARIF` | `SACR_UPLOAD_SARIF` |
| `ZREVIEW_ORG_RULES_REPO` | `SACR_ORG_RULES_REPO` |
| `ZREVIEW_CHEAP_MODEL` | `SACR_CHEAP_MODEL` |
| `ZREVIEW_LOG_FORMAT` | `SACR_LOG_FORMAT` |
| `ZREVIEW_OVERLAP_ENABLED` | `SACR_OVERLAP_ENABLED` |
| `~/.zreview/` | `~/.sacr/` |
| `<repo>/.zreview/*.yaml` | `<repo>/.sacr/*.yaml` |
| `OCR_LLM_URL` / `OCR_LLM_TOKEN` / `OCR_LLM_MODEL` (direct-endpoint override) | `SACR_LLM_URL` / `SACR_LLM_TOKEN` / `SACR_LLM_MODEL` |
| `~/.opencodereview/config.json` (legacy override path) | `~/.sacr/config.json` |
| `<!-- ZREVIEW:BEGIN/END -->` in PR bodies | `<!-- SACR:BEGIN/END -->` |
| `zreview-*` npm packages | `sacr` |
| `Formula/zreview.rb` (Homebrew) | `Formula/sacr.rb` |
| Repo `shubam-disseqt/z-code-reviewer` | `shubam-disseqt/shubam-ai-code-reviewer` |

Existing PRs with old `<!-- ZREVIEW:BEGIN/END -->` marker blocks will not be updated in-place by the new binary — the marker regex changed. Manual cleanup of stale zreview blocks (one-time) may be needed on long-lived PRs before re-running `sacr review` on them.

## Docs

- Live docs: **https://docs.sacr.dev** (post-deploy)
- Offline: `sacr docs` serves an embedded HTML viewer

## Verified in this release

- `go build ./...` clean
- `go test ./...` clean (all 34 packages)
- `go test -race` clean on the touched packages
- govulncheck: no vulnerabilities in tracked deps
- Windows parity CI: primary "Test with race" step green (informational verbose step marked non-blocking)

## Known limitations

- **Static-export docs (GH Pages):** the docs site builds against `docs-site/next.config.mjs` without `output: 'export'`. GH Pages deploys emit a placeholder page unless the option is enabled; enabling it disables the `/api/search` route. Vercel is the recommended deploy target for the docs site.
- **VS Code extension** shipped as a skeleton (`ide/vscode/`), not yet published to the Marketplace.
- **Manifest data** populates the `package_manifests` table but has no read-side consumer yet (feature parity for future vuln correlation).

## Attribution

Upstream attribution retained per Apache-2.0 §4(c)/(d) in the repo-root `NOTICE` file and in per-file SPDX headers on Go source. Neither is user-facing in the CLI or the docs.
