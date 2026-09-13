# z-code-reviewer

> AI-powered code reviewer for pull requests. Precise per-diff comments,
> repo-wide context, org-level review rules, and cross-PR overlap detection
> — as a single Go CLI, run locally or in CI.

**Status: Phase 2 skeleton.** The CLI compiles, `zreview version` and
`zreview docs` work. Review, indexing, rules, and overlap features are on
the roadmap and being built out. See [ROADMAP.md](ROADMAP.md).

## What it is

`z-code-reviewer` (binary: `zreview`) exists to give teams a single,
self-hosted, Go-native CLI that combines the best of two Apache-2.0 open
source projects:

| Feature | Source of the approach |
|---|---|
| Precise per-diff comments (deterministic file selection + line snapping + comment repair) | [alibaba/open-code-review](https://github.com/alibaba/open-code-review) |
| Repo-wide context via a persistent index | [miracodeai/mira](https://github.com/miracodeai/mira) |
| Business rules at org level (YAML in a git-hosted rules repo) | Adapted from Mira's `custom_rules` injection |
| Cross-PR overlap and merge-conflict detection | [miracodeai/mira](https://github.com/miracodeai/mira) |

Nothing else. No dashboard, no webhook server, no learning loop, no
vulnerability scanning, no delegation mode, no plugin marketplace.

The full attribution and per-file port map lives in [NOTICE](NOTICE) and
[PORTING.md](PORTING.md).

## Architecture

Read [ARCHITECTURE.md](ARCHITECTURE.md) for the pipeline, package map,
trust boundaries, and threat model. The same content is bundled with the
binary — run `zreview docs` for an offline copy.

## Quickstart

Once binaries ship (Phase 9):

```sh
# Install
curl -fsSL https://raw.githubusercontent.com/shubam-disseqt/z-code-reviewer/main/install.sh | sh

# Configure a provider (Anthropic / OpenAI / Bedrock / etc.)
export ANTHROPIC_API_KEY=...

# Review the current diff
cd your-project
zreview review --from main --to HEAD --format json --output result.json

# Serve the docs locally
zreview docs
```

For now, only the last of those actually runs. See [ROADMAP.md](ROADMAP.md).

## Build from source

```sh
git clone https://github.com/shubam-disseqt/z-code-reviewer.git
cd z-code-reviewer
make build
./bin/zreview version
./bin/zreview docs
```

## Development

- Go 1.24+
- `make check` runs the same battery as CI: `gofmt`, `go vet`, `go test`
- `make coverage` targets 80%+ per package
- LF line endings enforced via `.gitattributes`
- See [CONTRIBUTING.md](CONTRIBUTING.md), [AGENTS.md](AGENTS.md) for the
  contribution rules (including rules for AI-assisted contributions).

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

## Security

Vulnerabilities → private disclosure via GitHub Security Advisories on
this repo. See [SECURITY.md](SECURITY.md). The threat model lives in
[ARCHITECTURE.md § Trust boundaries and threats](ARCHITECTURE.md#trust-boundaries-and-threats).
