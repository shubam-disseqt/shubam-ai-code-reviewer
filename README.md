# z-code-reviewer

> **AI-powered PR reviewer as a single Go CLI.** Deterministic file
> selection + line-snapped comments + org-level rules + cross-PR overlap
> detection. Five LLM providers, offline docs, SLSA-attested releases.

<p>
  <a href="https://github.com/shubam-disseqt/z-code-reviewer/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/shubam-disseqt/z-code-reviewer/actions/workflows/ci.yml/badge.svg" /></a>
  <a href="https://github.com/shubam-disseqt/z-code-reviewer/actions/workflows/govulncheck.yml"><img alt="govulncheck" src="https://github.com/shubam-disseqt/z-code-reviewer/actions/workflows/govulncheck.yml/badge.svg" /></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg" /></a>
  <img alt="Go 1.26+" src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go" />
  <img alt="Linux" src="https://img.shields.io/badge/Linux-supported-blue.svg" />
  <img alt="macOS" src="https://img.shields.io/badge/macOS-supported-blue.svg" />
  <img alt="Windows" src="https://img.shields.io/badge/Windows-supported-blue.svg" />
</p>

## What is z-code-reviewer?

`z-code-reviewer` (binary: `zreview`) is a Go CLI that reads a git diff,
sends changed files to a configurable LLM through a small agent loop
with file-read / code-search / file-find / comment tools, and produces
review comments that land on the correct line, in the correct file, and
respect your team's rules.

Beyond the diff, `zreview` can:

- consult a **persistent code index** (SQLite or Postgres) for
  repo-wide context on files the diff touches, and
- scan other open PRs for **overlap or merge-conflict risk** before you
  hit merge.

There is no dashboard, no webhook server, no long-running service. One
binary, run locally or in CI.

## Why

If you've asked a general-purpose coding agent to review a PR, you've
likely hit three problems:

- **Incomplete coverage** — on larger changesets the agent quietly
  drops files.
- **Position drift** — the comment says "line 42" but the issue is
  actually on line 47.
- **Unstable quality** — a small prompt tweak changes the review.

`zreview` treats those as engineering problems, not prompt problems.

### Deterministic engineering × agent

The parts that *must not go wrong* are done by code, not by the model:

- **File selection** — a deterministic selector decides which changed
  files enter review and which are filtered (vendored code,
  generated code, size caps, path filters).
- **Line snapping** — every LLM-produced comment is post-processed
  against the actual hunk. Comments that can't be snapped to a real
  changed line are dropped rather than left drifting.
- **Rule matching** — org-level rules from a YAML repo are filtered by
  glob scope *before* the prompt is built, so the model sees a
  focused rule set instead of hundreds of irrelevant lines.
- **Comment repair** — malformed JSON tool calls from the model are
  repaired deterministically instead of triggering another round trip.

The **agent** does what only agents do well:

- decide which tool to call next (file read / code search / file find);
- pull just-in-time context when the index has no coverage;
- write the actual review comment.

The full pipeline, package map, and threat model live in
[ARCHITECTURE.md](ARCHITECTURE.md). The same content is embedded in the
binary — run `zreview docs` for the offline copy.

Historical debt: the deterministic-engineering pieces and prompts are
ported (with attribution) from
[alibaba/open-code-review](https://github.com/alibaba/open-code-review);
the index + overlap layers come from
[miracodeai/mira](https://github.com/miracodeai/mira). Full port map in
[PORTING.md](PORTING.md) and [NOTICE](NOTICE).

## Features

| | |
|---|---|
| **Precise per-diff comments** | deterministic file selector, line-snap post-processor, comment repair, dedup pass |
| **Repo-wide context** | `zreview index` builds a SQLite/Postgres code index; JIT context when the index is empty |
| **Org rules** | YAML in a git-hosted rules repo, filtered by glob scope, injected into the prompt |
| **Overlap detection** | `zreview overlap` finds other open PRs touching the same files/symbols |
| **Session log + resume** | append-only JSONL under `~/.zreview/sessions/`, resume with `--resume` |
| **5 LLM providers** | Anthropic, OpenAI (Chat Completions + Responses), AWS Bedrock, DeepSeek |
| **Output formats** | `stdout`, `json`, `github` (posts inline PR comments) |
| **Offline docs** | `zreview docs` serves 14 hand-written pages from `//go:embed`, strict CSP, loopback bind |
| **SLSA-attested releases** | matrix binaries + SHA-256 sums + [SLSA build provenance](https://slsa.dev/) |
| **Distribution** | install script (POSIX + PowerShell), npm platform-stub packages, Docker image, GitHub Action |
| **Doctor** | `zreview doctor` fails fast on misconfig in CI |
| **Threat-modelled** | see [THREAT_MODEL.md](THREAT_MODEL.md) for assets, boundaries, threats, mitigations |

## Install

Pick one:

```sh
# POSIX (Linux + macOS)
curl -fsSL https://raw.githubusercontent.com/shubam-disseqt/z-code-reviewer/main/install.sh | sh

# Windows PowerShell
iwr https://raw.githubusercontent.com/shubam-disseqt/z-code-reviewer/main/install.ps1 -useb | iex

# npm (Linux / macOS / Windows)
npm install -g zreview

# Docker
docker run --rm -v "$PWD:/repo" ghcr.io/shubam-disseqt/z-code-reviewer:latest review --repo /repo
```

For the GitHub Action, [PILOT.md](PILOT.md) has a ready-to-paste
workflow.

## Quick Start

```sh
# 1. Configure a provider (pick one)
export ANTHROPIC_API_KEY=sk-ant-...
# or OPENAI_API_KEY, DEEPSEEK_API_KEY, or ambient AWS creds for Bedrock

# 2. Sanity check
zreview doctor

# 3. Review the current diff
cd your-project
zreview review --from main --to HEAD                    # stdout
zreview review --from main --to HEAD --format json      # machine-readable
zreview review --pr 42 --format github                  # post inline on PR

# 4. Build a repo index (optional, better context on larger repos)
zreview index --repo . --backend sqlite --path .zreview/index.db

# 5. Detect PR overlap
zreview overlap --pr 42

# 6. Sync org rules
export ZREVIEW_ORG_RULES_REPO=git@github.com:your-org/zreview-rules.git
zreview rules sync
zreview rules list

# 7. Read the offline docs
zreview docs
```

The docs viewer opens on a random loopback port with a strict CSP —
nothing calls out to the internet.

## Documentation

Full documentation is available offline via `zreview docs`, or read the
raw HTML in [`docs/`](docs/):

- [Quickstart](docs/quickstart.html) — install and run your first review
- [Installation](docs/installation.html) — every platform and package
  manager
- [Configuration](docs/configuration.html) — every env var and config
  key
- [CLI Reference](docs/cli-reference.html) — every command and flag
- [Providers](docs/providers.html) — the 5 supported LLM providers,
  model IDs, cost notes
- [Index and context](docs/index-and-context.html) — SQLite vs
  Postgres, JIT context
- [Review rules](docs/review-rules.html) — org rules layer, glob scope,
  YAML schema
- [Overlap](docs/overlap.html) — cross-PR overlap detection
- [Session log](docs/session-log.html) — JSONL format, `--resume`
  semantics
- [GitHub Action](docs/github-action.html) — drop the composite action
  into a workflow
- [Docs deployment](docs/deployment.html) — Cloudflare Pages setup for
  the public docs site (works on private repos too)
- [Architecture](docs/architecture.html) — pipeline, package map,
  trust boundaries (mirrors [ARCHITECTURE.md](ARCHITECTURE.md))
- [Security](docs/security.html) — mirrors [SECURITY.md](SECURITY.md)
  and [THREAT_MODEL.md](THREAT_MODEL.md)
- [Troubleshooting](docs/troubleshooting.html) — the top hits

## Build from source

```sh
git clone https://github.com/shubam-disseqt/z-code-reviewer.git
cd z-code-reviewer
make build
./bin/zreview version
./bin/zreview docs
```

## Development

- Go 1.26.2+
- `make check` runs the same battery as CI: tidy + gofmt + vet + race
  test suite
- `make coverage` targets ≥80% per package (currently 88–100% except
  `cmd/zreview` at 62%)
- `make vuln` runs `govulncheck` locally (weekly cron in CI)
- LF line endings enforced via `.gitattributes`
- See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) —
  the latter is the contribution policy for AI-assisted PRs
  (mandatory: disclose the model, no attribution trailers, no
  fixup-fixup-fixup histories).

## Roadmap

`v0.1.0` ships the full feature set. `v1.0.0` follows after a one-week
pilot on a real repo — playbook in [PILOT.md](PILOT.md), remaining
items and phase-by-phase history in [ROADMAP.md](ROADMAP.md).

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Security

Vulnerabilities → private disclosure via GitHub Security Advisories on
this repo. See [SECURITY.md](SECURITY.md). The threat model lives in
[THREAT_MODEL.md](THREAT_MODEL.md).
