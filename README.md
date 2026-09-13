<div align="center">
  <h1>z-code-reviewer</h1>
  <p><strong>An AI-powered code review CLI &mdash; deterministic engineering wrapped around a thin agent loop.</strong></p>
</div>

<p align="center">
  <a href="https://github.com/shubam-disseqt/z-code-reviewer/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/shubam-disseqt/z-code-reviewer/actions/workflows/ci.yml/badge.svg" /></a>
  <a href="https://github.com/shubam-disseqt/z-code-reviewer/actions/workflows/govulncheck.yml"><img alt="govulncheck" src="https://github.com/shubam-disseqt/z-code-reviewer/actions/workflows/govulncheck.yml/badge.svg" /></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg?style=flat-square" /></a>
  <img alt="Go 1.26+" src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&style=flat-square" />
  <img alt="SLSA build provenance" src="https://img.shields.io/badge/SLSA-build--provenance-D4AF37?style=flat-square" />
</p>
<p align="center">
  <a href="#supported-platforms"><img alt="Windows" src="https://img.shields.io/badge/Windows-supported-blue.svg?style=flat-square" /></a>
  <a href="#supported-platforms"><img alt="macOS" src="https://img.shields.io/badge/macOS-supported-blue.svg?style=flat-square" /></a>
  <a href="#supported-platforms"><img alt="Linux" src="https://img.shields.io/badge/Linux-supported-blue.svg?style=flat-square" /></a>
</p>
<p align="center">
  <a href="#providers"><img alt="Anthropic" src="https://img.shields.io/badge/Anthropic-supported-blueviolet.svg?style=flat-square" /></a>
  <a href="#providers"><img alt="OpenAI" src="https://img.shields.io/badge/OpenAI-supported-blueviolet.svg?style=flat-square" /></a>
  <a href="#providers"><img alt="AWS Bedrock" src="https://img.shields.io/badge/AWS%20Bedrock-supported-blueviolet.svg?style=flat-square" /></a>
  <a href="#providers"><img alt="DeepSeek" src="https://img.shields.io/badge/DeepSeek-supported-blueviolet.svg?style=flat-square" /></a>
</p>
<p align="center">
  English | <em>(more languages TBD)</em>
</p>

---

## What is z-code-reviewer?

`z-code-reviewer` (binary: `zreview`) is an AI-powered pull request reviewer that runs as a single Go binary. It reads Git diffs, sends changed files through a small tool-using agent loop, and produces structured review comments that land on the correct file and line, respect your team's rules, and never drown a PR in noise.

Beyond diff review, `zreview` also **indexes your codebase** for repo-wide context, **runs deterministic security scanners** (Gitleaks + Semgrep + govulncheck) alongside the LLM, **scores every finding** against a configurable severity policy, and **detects cross-PR overlap** before merges collide.

Its design philosophy is inherited from two Apache-2.0 upstream projects. The diff-precision layer, the agent tool loop, and the prompt templates are ported (with attribution) from [alibaba/open-code-review](https://github.com/alibaba/open-code-review) &mdash; battle-tested inside Alibaba Group as its official AI review assistant across tens of thousands of developers. The persistent code index, JIT context builder, and cross-PR overlap detector are re-implemented in Go from [miracodeai/mira](https://github.com/miracodeai/mira). Full port map: [docs/PORTING.md](docs/PORTING.md) and [NOTICE](NOTICE).

![Highlights](docs/architecture.html)

## Why z-code-reviewer?

### The problem with general-purpose agents

If you've asked a general-purpose coding agent (Claude Code, Cursor, Copilot) to review a pull request, you've likely hit three problems:

- **Incomplete coverage** &mdash; on larger changesets, agents cut corners and quietly drop files.
- **Position drift** &mdash; the reported line number often doesn't match where the issue actually lives.
- **Unstable quality** &mdash; small prompt changes swing review output; there are no hard constraints.

The root cause: a purely language-driven architecture has no hard guarantees around the review process.

### Core design: deterministic engineering &times; agent hybrid

`zreview` treats these as engineering problems, not prompt problems. Steps that *must not go wrong* are done by code. Steps that need adaptive judgement are done by the LLM. Neither can replace the other.

**Deterministic engineering &mdash; hard constraints**

- **Precise file selection.** A deterministic selector decides which files enter review and which are filtered (vendored code, generated code, size caps, path filters). No agent-driven "I'll skip this one."
- **Line snapping.** Every LLM-produced comment is post-processed against the actual diff hunk. Comments that can't be snapped to a real changed line are dropped rather than shipped drifting.
- **Rule matching.** Org-level YAML rules are filtered by glob scope *before* the prompt is built. The model sees a focused rule set, not hundreds of irrelevant lines.
- **Comment repair.** Malformed JSON tool calls from the model are repaired deterministically instead of triggering another round trip.
- **Fingerprinting + incremental re-review.** Findings persist across pushes to the same PR. Files whose findings are already fixed drop out. Only affected scope is re-reviewed.
- **Severity scoring.** A deterministic YAML policy assigns `CRITICAL / HIGH / MEDIUM / LOW / SUPPRESS` from confidence &times; impact &times; category. Not the LLM's opinion.

**Agent &mdash; dynamic decision-making**

- **Scenario-tuned prompts.** Distilled from Alibaba OCR's production traces; not a generic assistant prompt.
- **Purpose-built toolset.** `file_read`, `file_read_diff`, `file_find`, `code_search`, `code_comment`, `task_done`. That's it &mdash; a small tool surface tuned specifically for code review, not a full agent toolkit.

## Supported platforms

| Platform | Architecture | Install |
|---|---|---|
| Linux | amd64, arm64 | install.sh, Docker, npm |
| macOS | amd64, arm64 | install.sh, Homebrew (TBD), npm |
| Windows | amd64, arm64 | install.ps1, npm |

Every release ships with SHA-256 checksums and [SLSA build-provenance](https://slsa.dev/) attestation.

## Providers

Five LLM providers wired end-to-end. Any one works &mdash; pick the one your team already pays for.

| Provider | Env var | Notes |
|---|---|---|
| Anthropic Claude | `ANTHROPIC_API_KEY` | Sonnet-class recommended for the reviewer tier |
| OpenAI Chat Completions | `OPENAI_API_KEY` | GPT-5.x family |
| OpenAI Responses | `OPENAI_RESPONSES_API_KEY` | GPT-5.6 family |
| AWS Bedrock (Anthropic models) | *ambient AWS creds* | SigV4, no static key |
| DeepSeek | `DEEPSEEK_API_KEY` | Cheapest of the four |

**Two-tier routing.** Set `ZREVIEW_CHEAP_MODEL` (and optionally `ZREVIEW_CHEAP_PROVIDER`) to route the summarizer and labeler to a smaller model while keeping the reviewer on your Sonnet-class model. Measured savings: 30&ndash;40% per review.

## How to use

### Prerequisites

- **Git &ge; 2.41** &mdash; `zreview` relies on git for diff generation and repository operations.
- A provider credential from the table above.
- Optional but recommended: `gitleaks`, `semgrep`, `govulncheck` in `$PATH` for the deterministic security-scanner tier.

### Install

```bash
# npm (Linux / macOS / Windows)
npm install -g zreview

# Homebrew (macOS)  (TBD)
brew install zreview

# POSIX install script
curl -fsSL https://raw.githubusercontent.com/shubam-disseqt/z-code-reviewer/main/scripts/install.sh | sh

# Windows PowerShell
iwr https://raw.githubusercontent.com/shubam-disseqt/z-code-reviewer/main/scripts/install.ps1 -useb | iex

# Docker
docker run --rm -v "$PWD:/repo" ghcr.io/shubam-disseqt/z-code-reviewer:latest review --repo /repo
```

After installation, the `zreview` command is available globally.

### Quick start

**1. Configure a provider**

```bash
export ANTHROPIC_API_KEY=sk-ant-...
# or OPENAI_API_KEY, DEEPSEEK_API_KEY, or ambient AWS creds for Bedrock
zreview doctor          # verifies provider + git + scanners in one line
```

**2. Review**

```bash
cd your-project

# Workspace mode — review the current uncommitted diff
zreview review

# Branch range — reviews feature-branch's changes since it diverged from main
zreview review --from main --to feature-branch

# Single commit
zreview review --commit abc123

# Post inline comments on a real PR
zreview review --pr 42 --format github

# Machine-readable JSON output (recommended for host agents / dashboards)
zreview review --format json --output result.json

# SARIF output for GitHub Code Scanning (security findings)
zreview review --format sarif --output findings.sarif

# Resume an interrupted session
zreview review --resume <session-id>
```

**3. Build an index (optional &mdash; better context on large repos)**

```bash
export ZREVIEW_DB_URL=sqlite:///.zreview/index.db
zreview index --repo .
```

**4. Detect cross-PR overlap**

```bash
export GITHUB_TOKEN=ghp_...
zreview overlap --pr 42
```

**5. Sync org rules**

```bash
export ZREVIEW_ORG_RULES_REPO=git@github.com:your-org/zreview-rules.git
zreview rules sync
zreview rules list
```

**6. Read the docs**

```bash
zreview docs        # serves the offline site on a random loopback port
```

### GitHub Action

Drop the composite action into a workflow to review every pull request automatically:

```yaml
name: AI review
on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  contents: read
  pull-requests: write

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: shubam-disseqt/z-code-reviewer@v0
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
```

Full setup, including Code Scanning SARIF upload and custom rules, in [docs/github-action.html](docs/github-action.html).

## Documentation

Full documentation is bundled with the binary &mdash; run `zreview docs` to browse offline. The raw HTML lives in [`docs/`](docs/):

- [Quickstart](docs/quickstart.html) &mdash; install and run your first review
- [Installation](docs/installation.html) &mdash; every platform and package manager
- [Configuration](docs/configuration.html) &mdash; every env var and config key
- [CLI reference](docs/cli-reference.html) &mdash; every command and flag
- [Providers](docs/providers.html) &mdash; model IDs, tier routing, cost notes
- [Index and context](docs/index-and-context.html) &mdash; SQLite / Postgres index; JIT context
- [Review rules](docs/review-rules.html) &mdash; YAML schema, glob scoping, injection
- [Overlap](docs/overlap.html) &mdash; cross-PR overlap detection
- [Scanners](docs/scanners.html) &mdash; Gitleaks / Semgrep / govulncheck integration
- [SARIF output](docs/sarif.html) &mdash; GitHub Code Scanning upload
- [Session log](docs/session-log.html) &mdash; JSONL format, `--resume`, redaction
- [Reliability](docs/reliability.html) &mdash; retry policy, rate limiting, env-var knobs
- [GitHub Action](docs/github-action.html) &mdash; drop-in composite action
- [Testing](docs/testing.html) &mdash; running the e2e test manually
- [Troubleshooting](docs/troubleshooting.html) &mdash; the top hits, structured logging, metrics
- [Architecture](docs/architecture.html) &mdash; pipeline, package map, trust boundaries
- [Security](docs/security.html) &mdash; mirrors [SECURITY.md](SECURITY.md) and [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md)

## Development

```bash
git clone https://github.com/shubam-disseqt/z-code-reviewer.git
cd z-code-reviewer
make build              # builds bin/zreview
./bin/zreview version
./bin/zreview docs
```

- Go 1.26.2+
- `make check` runs the CI battery: tidy + gofmt + vet + race test suite
- `make coverage` &mdash; per-package coverage; ships 88&ndash;100% on all business logic
- `make vuln` &mdash; runs `govulncheck` locally (weekly cron in CI)
- LF line endings enforced via `.gitattributes`
- See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) &mdash; the latter is the contribution policy for AI-assisted PRs (mandatory: disclose the model, no attribution trailers, no fixup-fixup-fixup histories)

## Roadmap

`v0.1.0` ships the full feature set including scanners, scoring, SARIF, and incremental re-review. `v1.0.0` follows after a one-week pilot on a real repo. Remaining items and phase-by-phase history live in [ROADMAP.md](ROADMAP.md).

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Security

Vulnerabilities: private disclosure via GitHub Security Advisories on this repo. See [SECURITY.md](SECURITY.md) for the reporting policy and [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) for the shipped mitigations.

## Acknowledgements

`zreview` would not exist without the work of two upstream open-source projects, both under Apache-2.0:

- [alibaba/open-code-review](https://github.com/alibaba/open-code-review) &mdash; the diff-precision layer, tool loop, prompt templates, and comment-args-repair logic.
- [miracodeai/mira](https://github.com/miracodeai/mira) &mdash; the persistent code index, JIT cross-file context, cross-PR overlap detector, and business-rules injection surface.

Per-file attribution: [NOTICE](NOTICE). Detailed port map: [docs/PORTING.md](docs/PORTING.md).
