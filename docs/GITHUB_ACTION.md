# GitHub Action

Drop-in reusable action that runs `zreview` on every pull request. The action
wraps the [published Docker image](DOCKER.md) so downstream repos don't need Go
installed and don't need to think about install scripts.

## Minimal usage

Create `.github/workflows/review.yml` in your repo:

```yaml
name: zreview
on:
  pull_request:
    types: [opened, synchronize, reopened]

jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      pull-requests: write
      contents: read
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0        # zreview needs the base ref
      - uses: shubam-disseqt/z-code-reviewer@v1
        with:
          pr-number: ${{ github.event.pull_request.number }}
          api-key: ${{ secrets.OPENAI_API_KEY }}
          github-token: ${{ secrets.GITHUB_TOKEN }}
```

That's it. Three lines of `with:` and every PR gets an inline review.

## Inputs

| Input          | Default                                     | Notes |
|----------------|---------------------------------------------|-------|
| `pr-number`    | *(required)*                                | Usually `${{ github.event.pull_request.number }}`. |
| `api-key`      | *(required)*                                | Routed to the provider's env var. |
| `github-token` | *(required)*                                | `secrets.GITHUB_TOKEN` is enough for public repos. |
| `provider`     | `openai`                                    | `openai` \| `anthropic` \| `deepseek` \| `bedrock` |
| `model`        | `gpt-4o-mini`                               | Main-tier model. |
| `cheap-model`  | *(unset → falls back to `model`)*           | Cheap-tier model for triage passes. |
| `min-severity` | `MEDIUM`                                    | `LOW` \| `MEDIUM` \| `HIGH` \| `CRITICAL` |
| `format`       | `github`                                    | `stdout` \| `json` \| `github` \| `sarif` |
| `image`        | `ghcr.io/shubam-disseqt/zreview:latest`     | Pin to a version tag in production. |

## Pinning in production

```yaml
- uses: shubam-disseqt/z-code-reviewer@v1
  with:
    pr-number: ${{ github.event.pull_request.number }}
    image: ghcr.io/shubam-disseqt/zreview:v1.0.0
    api-key: ${{ secrets.OPENAI_API_KEY }}
    github-token: ${{ secrets.GITHUB_TOKEN }}
```

Pinning the image + a version tag on the action (or a full commit SHA) is the
safe combination for compliance-sensitive repos.

## Provider switching

```yaml
with:
  provider: anthropic
  model: claude-sonnet-4-6
  api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

## What the action does under the hood

1. Routes `api-key` into the correct provider env var (`OPENAI_API_KEY`,
   `ANTHROPIC_API_KEY`, `DEEPSEEK_API_KEY`, `AWS_BEARER_TOKEN_BEDROCK`).
2. Runs `docker run --rm` against the pinned image, mounting `$GITHUB_WORKSPACE`
   at `/workspace`.
3. Invokes `zreview review --pr <n> --format <fmt> --min-severity <bucket>`.
4. The `github` formatter posts inline review comments via the GitHub API using
   `GITHUB_TOKEN`.

## Fork PRs

`secrets.*` are not available on `pull_request` runs from forks. Two options:

- Use `pull_request_target` **only if** you understand the associated
  privilege-escalation risks (do not `checkout` untrusted refs on that trigger).
- Gate the job on `github.event.pull_request.head.repo.full_name == github.repository`.
