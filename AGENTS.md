# Rules for AI-assisted contributions

This file governs contributions to `z-code-reviewer` made with the help
of AI coding assistants. It applies to humans using AI, and equally to
autonomous AI agents opening PRs directly.

## Non-negotiable rules

1. **Disclose AI/LLM use** in the initial issue or PR body. Name the
   tools and models used (e.g., "Claude Code with Sonnet 4.6"). One
   line is enough.

2. **You must understand every line of code you submit**, including
   the parts an AI wrote. If a reviewer asks why a change is written
   the way it is, the substance of your answer must come from your
   own understanding. AI/LLM is welcome for translation and polish;
   it is not welcome as a proxy for comprehension.

3. **No `AI generated → fixed → fixed → fixed` cycles** in your PR
   history. That pattern signals unreviewed AI output. Squash locally
   before requesting review.

4. **Self-review AI output before requesting human review.** If you
   would not merge it yourself, do not ask someone else to.

5. **No AI attribution trailers.** No `Co-Authored-By: Claude ...`,
   no `Assisted-by:`. Attribution goes in the PR body or design doc
   when material, not in commit metadata. This mirrors the upstream
   OCR project's rule.

6. **Keep commit messages short.** Details go in the PR body, not in
   collapsed commit footers.

7. **You may not commit changes to `LICENSE` or `NOTICE` via
   AI-generated PRs without a human maintainer's second review.**
   Both are legally load-bearing.

## Process rules for AI-assisted work

- Every AI-generated PR must pass `make check` locally before
  submission. CI will run it again — that is not a substitute for a
  local pass.
- If the AI's output touches attribution, licensing, or the porting
  map in [docs/PORTING.md](docs/PORTING.md), flag it explicitly in the PR body.
- The project uses `zreview review` on itself when v1 ships. Until
  then, AI-generated PRs receive extra scrutiny by convention.

## Rules for autonomous AI agents opening PRs

Direct-PR agents (Devin, sweep.dev, similar) must:

- Identify themselves in the PR body — model, harness, and prompt link
  where possible.
- Not open more than 3 open PRs at a time.
- Not open PRs that revert a maintainer's decision without a linked
  issue explaining new evidence.
- Not modify `AGENTS.md`, `SECURITY.md`, `GOVERNANCE.md`, `LICENSE`,
  or `NOTICE` unless the PR is explicitly labeled `governance:review-needed`
  and a human maintainer opens it.

## When these rules would block you

If you are not willing or able to meet the rules above, please close
the issue or pull request. The upstream OCR project's rule — "If you
are unwilling to do all of the above, please close the issue or PR" —
applies here too.
