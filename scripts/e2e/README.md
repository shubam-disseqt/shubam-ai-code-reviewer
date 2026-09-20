# sacr E2E matrix

Verifies `sacr` catches the seeded bugs on the `zreview-e2e-matrix` companion repo. Runs nightly (03:00 UTC) + on every `v*` tag push + on demand via `workflow_dispatch`.

## How the matrix repo is organized

- Each PR archetype lives on a branch named `pr-N-branch` (Nov: `rollup-mega` for the combined stress case).
- Every enabled branch has `.matrix/expected.json` at repo root — the ground truth of what `sacr` MUST catch.
- The branch's HEAD commit is what we review (`sacr review --commit HEAD`).

## expected.json schema

```json
{
  "case": "pr-3-branch",
  "description": "human-readable summary of the archetype",
  "findings": [
    {
      "path": "pricing.go",
      "start_line": 9,
      "end_line": 9,
      "category": "security",
      "min_severity": "critical",
      "description": "hardcoded API key",
      "soft": false
    }
  ]
}
```

Match rules (see `scripts/e2e/assert/assert.go`):

1. Category equal (case-insensitive)
2. Path equal (exact)
3. Actual line range overlaps `[start_line - 3, end_line + 3]` (LineTolerance = 3 for LLM drift)
4. Actual severity `>=` `min_severity`

Extra findings (no match) are reported as noise but do not fail a case.

## Local run

```bash
# From product repo root, against a matrix checkout at /tmp/e2e-matrix
export OPENAI_API_KEY=sk-...
export SACR_MODEL=gpt-4o-mini

# 1) Build sacr + tooling
go build -o /tmp/sacr ./cmd/sacr
go build -o /tmp/assert ./scripts/e2e/assert
go build -o /tmp/aggregate ./scripts/e2e/aggregate

# 2) For each case:
cd /tmp/e2e-matrix
git checkout pr-3-branch
/tmp/sacr review --commit HEAD --format json --min-severity LOW --output /tmp/actual.json
/tmp/assert -expected .matrix/expected.json -actual /tmp/actual.json -json > /tmp/result-pr-3.json

# 3) Aggregate:
/tmp/aggregate -min-recall 0.8 -markdown /tmp/report.md /tmp/result-pr-*.json
cat /tmp/report.md
```

## Adding a case

1. On the matrix repo, create a branch with the archetype code (feat commit) + optional fix commit for reference
2. Add `.matrix/expected.json` describing every seeded bug
3. In `.github/workflows/e2e-matrix.yml` add the branch name to the `CASES` env var
4. Trigger the workflow manually (`gh workflow run e2e-matrix.yml`)
5. Read the report; iterate on `expected.json` if `sacr` catches the bug at a slightly different line/category

## Gate

- `hard recall >= 80%` (aggregate across all cases' non-soft expected findings)
- **AND** no seeded `critical` finding is missed on any case

Below either bar → workflow fails.

## Required secrets on the product repo

| Secret | Purpose |
|---|---|
| `OPENAI_API_KEY` **or** `ANTHROPIC_API_KEY` | LLM provider for the review call. Only one is required; the workflow passes both through. |
| `E2E_MATRIX_TOKEN` | fine-grained PAT with `contents: read` on `shubam-disseqt/zreview-e2e-matrix` (needed because that repo is private and the default `GITHUB_TOKEN` is scoped to the product repo). |

## Cost expectations

At `gpt-4o-mini`:

- ~5-15k input tokens per case × 4 cases = ~$0.05 per matrix run
- Nightly + release tags ≈ 400 runs/year ≈ **~$20/year**

If a run misprints logs a much higher number in `sacr metrics: cost=…`, investigate — a prompt regression may have exploded token counts.
