# Pilot playbook

`zreview` cuts v0.1.0 with a supported feature set but no field data.
Phase 11 in [ROADMAP.md](ROADMAP.md) is a one-week pilot against a real
production repo to tune noise, precision, and cost before v1.0.0.

## Default target

`disseqt-auth-service` — small Go service, active PR flow, low blast
radius if the reviewer misbehaves. Any other disseqt repo works too;
this file names one so the pilot has a default.

## Setting up the pilot

Add one workflow to the target repo:

```yaml
# .github/workflows/zreview-pilot.yml
name: AI review (pilot)
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
      - uses: shubam-disseqt/z-code-reviewer@v0.1.0
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        with:
          from: ${{ github.event.pull_request.base.sha }}
          to:   ${{ github.event.pull_request.head.sha }}
          format: github
```

Required repo secrets: `ANTHROPIC_API_KEY` (or one of the other four
supported providers — see [docs/providers.html](docs/providers.html)).

`fetch-depth: 0` is not optional. `zreview` needs both refs to compute
the diff locally; a shallow clone will fail with a clear error.

## What to measure during the pilot week

Track these per PR touched by the pilot:

- **True positive count** — comments the author acted on
- **False positive count** — comments the author dismissed
- **Line-precision hit rate** — did the comment land on the correct
  line, or drift by ≥1 line?
- **Median wall time** — from `pull_request` event to comments posted
- **Median LLM cost** — sum of provider usage per review (Anthropic
  returns token counts in its API response; the session JSONL records
  them — see [docs/session-log.html](docs/session-log.html))
- **Retry / resume count** — non-zero means we're papering over a bug

Ship a lightweight sheet (spreadsheet, Linear issue, or the pilot repo
itself) with one row per review. The point is to notice trends, not to
build dashboards.

## What triggers a bug-fix release during the pilot

- Any confirmed false positive rate above 30% for two consecutive days
- Any crash / panic in review pipeline
- Any leak of code snippets or secrets into logs or comments
- Line-precision hit rate under 90% (measured over ≥ 20 reviews)

Cut a `v0.1.x` patch, bump `@v0` in the target repo's workflow, keep
going.

## Exiting the pilot → v1.0.0

Cut v1.0.0 after **five consecutive days** of pilot use with:

- False positive rate under 20%
- No new crash-class bugs opened
- Line-precision hit rate ≥ 95%
- Median cost per review under $0.10 for the smallest configured
  provider tier (currently DeepSeek) and under $0.50 for Anthropic
  Sonnet-class models

If those bars aren't met after the first week, extend the pilot by
one week and iterate. Don't rush v1.0.0 to hit a calendar target.
