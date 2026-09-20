# sacr E2E matrix — PR mode

Verifies `sacr` detects seeded bugs by running the **actual production PR-review flow** against 4 persistent PRs on the companion repo `shubam-disseqt/zreview-e2e-matrix`.

Runs nightly (03:00 UTC) + on every `v*` tag push + on demand via `workflow_dispatch`.

## What "PR mode" means

Every case is a **persistent open PR** on the matrix repo. Each nightly run:

1. Product-repo workflow triggers the matrix repo's `sacr-review.yml` workflow with the PR number as input
2. Matrix workflow runs `sacr review --pr N --format github` which **posts inline comments to the PR** (via GitHub's Reviews API)
3. Matrix workflow ALSO runs `sacr review --commit HEAD --format json --output result.json` — captures the same-run findings for E2E assertion
4. Matrix workflow uploads `result.json` as an artifact
5. Product-repo workflow downloads the artifact, runs the `assert` binary against the branch's `.matrix/expected.json`, aggregates + gates on recall threshold

This tests both **bug detection** AND **the GitHub PR-posting flow**. Comments accumulate across runs but sacr's fingerprint markers (`<!-- sacr:fp:HEX -->`) let it identify and delete its own stale comments before each new post.

## Matrix repo layout

- `main` — clean baseline
- `pr-N-branch` — one branch per case archetype; branch's HEAD carries the seeded bug(s) and `.matrix/expected.json`
- **4 persistent open PRs** at time of writing (each `pr-N-branch → main`):

  | Case branch | PR # | Seeded bugs |
  |---|---|---|
  | `pr-1-branch` | 1 | 0 (docs-only overview) |
  | `pr-3-branch` | 6 | 5 (secret, dropped err, oob, XSS, ignored err) |
  | `pr-5-branch` | 3 | 2 (weak entropy, timing attack) |
  | `pr-10-branch` | 8 | 0 (deps-only bump) |

  PR numbers are **not hardcoded** — the workflow discovers them at runtime via `gh pr list --head <branch>`. Rename branches or recreate PRs freely; workflow adapts.

## expected.json schema

```json
{
  "case": "pr-3-branch",
  "description": "human-readable archetype summary",
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

Extra findings (no expected match) are reported as noise but do not fail a case.

## Required secrets

### On the product repo (`shubam-ai-code-reviewer`)

| Secret | Scope | Purpose |
|---|---|---|
| `E2E_MATRIX_TOKEN` | fine-grained PAT: `contents: read`, `actions: write`, `pull-requests: read` on `shubam-disseqt/zreview-e2e-matrix` | Trigger the matrix workflow, list PRs, download artifacts |

### On the matrix repo (`zreview-e2e-matrix`)

| Secret | Purpose |
|---|---|
| `OPENAI_API_KEY` **or** `ANTHROPIC_API_KEY` | LLM provider for sacr |
| `PRODUCT_REPO_TOKEN` | fine-grained PAT with `contents: read` on `shubam-ai-code-reviewer` — used to check out product repo and build sacr from source. Remove once v0.2.0 is tagged and the matrix workflow can `uses:` the published Action. |

## Local dry run (without triggering the matrix workflow)

```bash
# From product repo root
export OPENAI_API_KEY=sk-...
export SACR_MODEL=gpt-4o-mini

# 1) Clone matrix + check out one case
git clone git@github.com:shubam-disseqt/zreview-e2e-matrix.git /tmp/e2e
cd /tmp/e2e && git checkout pr-3-branch

# 2) Run sacr in JSON mode (no PR posting for local dev)
sacr review --commit HEAD --format json --min-severity LOW --output /tmp/actual.json --repo .

# 3) Assert
cd -
go build -o /tmp/assert ./scripts/e2e/assert
/tmp/assert -expected /tmp/e2e/.matrix/expected.json -actual /tmp/actual.json -case pr-3-branch
```

## Adding a case

1. On matrix repo, create branch `pr-N-branch` off `main` with the seeded bug and `.matrix/expected.json`
2. Open a persistent PR `pr-N-branch → main` and keep it open
3. Add `pr-N-branch` to the `CASES` env in `.github/workflows/e2e-matrix.yml` on the product repo
4. Trigger a manual run: `gh workflow run e2e-matrix.yml`

## Gate

- `hard recall >= 80%` (aggregate across all cases' non-soft expected findings)
- **AND** no seeded `critical` finding is missed on any case

Below either bar → workflow fails.

## Cost expectations

Two `sacr review` calls per case (post + capture-json), 4 cases per run:

- ~5-15k input tokens × 2 calls × 4 cases = ~$0.10 per matrix run at `gpt-4o-mini`
- Nightly + release tags ≈ 400 runs/year ≈ **~$40/year**

## What's deferred to follow-ups

- `pr-2/pr-4/pr-6/pr-7/pr-8/pr-9` — persistent PRs already open on matrix repo but no `.matrix/expected.json` yet. Each takes ~15 min to derive from the branch's feat commit.
- Swap the matrix workflow from `go build ./cmd/sacr` to `uses: shubam-disseqt/shubam-ai-code-reviewer@v0.2.0` once the Action is published to the Marketplace.
- Multi-provider matrix (run same 4 cases with Sonnet + gpt-4o-mini side by side).
