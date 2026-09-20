# Delete-first value-shift dogfood — results

**Method:** Applied the council's D-path (two-line value-shift into Suggestion Mode) and progressively strengthened the experiment across three runs against the same three recent commits. Every run used `--min-severity LOW` and JSON output.

## Three experiments, one signal

| # | Configuration | Delete-first findings | Any findings | Retries¹ |
|---|---|---:|---:|---:|
| **1** | `gpt-4o-mini` + value-shifted prompt | **0** | 1 (a `test` suggestion) | 5/26 |
| **2** | `gpt-4o-mini` + prompt + **evidence injection** (caller counts via `git grep`) | **0** | 4 (all `bug` / `test`) | 4/26 |
| **3** | `gpt-4o` + prompt + evidence + **explicit interpretation** ("Total occurrences 2, Files 1 = INLINE candidate") | **0** | 2 (one bug, one hallucinated bug²) | 6/26 |
| **Ground truth** (Sonnet-class Explore agents, same commits) | | **7** candidates | 7 | — |

¹ "No tool calls parsed for `<file>`, retrying..." — LLM produced output that didn't parse as a tool call.
² Run 3 flagged `buildReLocationTemplate()` as "referenced but not defined in the repository" — actually defined at `cmd/sacr/prompts.go:175`. False positive.

**Recall on delete-first category: 0/7 (0%) across all three configurations.**

## Diagnostic detail per experiment

**Experiment 1** — value-shift alone. LLM emitted a lone `test` suggestion ("consider adding test cases for invalid provider names"). The shift changed nothing measurable — same category and shape as baseline Suggestion Mode.

**Experiment 2** — added the `## Evidence (deterministic)` block containing raw `git grep` counts for every new symbol in the commit. Sample entries:
```
| isNonReviewablePath | 3 | 1 |
| chunkDirs | 2 | 1 |
| matchesLang | 4 | 1 |
```

**Diagnostic gold:** on commit 3426975, the model saw `resolveAmbientPreset: 3 occurrences, 1 file` in the evidence and emitted a finding about it — but classified it as `test`, "Missing test cases; suggest adding table-driven tests." **The model USED the evidence but drew the opposite conclusion from the intended one.** Low occurrence → "under-tested" instead of "over-abstracted."

**Experiment 3** — bumped to `gpt-4o` AND added explicit interpretation rules to the prompt: "`Total occurrences: 2, Files: 1` — the symbol is defined once and used once. **This is an INLINE candidate**, not an under-tested symbol." Result: still zero delete-first findings. gpt-4o additionally hallucinated one bug (claimed a defined function was undefined).

## What this rules out

1. ~~Model strength alone was the bottleneck~~ — gpt-4o with the same evidence produces the same recall on this category (0/7).
2. ~~Deterministic evidence solves the reasoning gap~~ — the model can consume caller counts but doesn't map "low count" to "inline candidate" without explicit interpretation. Even with explicit interpretation, still 0.
3. ~~The value-shift needs more prompt tokens~~ — adding ~40 more lines of guidance and interpretation across three iterations produced zero delta on the target category, while the retry rate held steady around 15-20%.

## What this DOES NOT rule out

- **A dedicated simplification-only LLM pass** — a separate call that ONLY looks for over-engineering, with a stripped-down prompt that doesn't compete with bug/security/test hunting. The current prompt asks the model to do many things; the simplification lens seems to lose the priority arbitration inside a single call.
- **Structured output for lens** — B's design (dedicated `lens` field on `LlmComment`) forces the model to fill a slot, which prompt-only cannot.
- **A different model family** — Claude Sonnet is not tested here (no key available). But given ground-truth agents were Sonnet-class and found candidates via static analysis, this may collapse to "Sonnet finds them, GPT doesn't."

## Honest verdict

The council's D-path (add value-shift to Suggestion Mode) is DEAD on OpenAI models. Three cumulative strengthenings — evidence injection, explicit interpretation, gpt-4o — did not change delete-first-category recall. The 4-comment run in experiment 2 shows the model can be nudged to *look* at low-count symbols, but always toward "add tests" or "add checks," never toward "inline this."

Peer review #3's warning applies with fresh weight: nobody validated demand for delete-suggestions before this experiment, and now we have evidence that even a stronger model with hand-fed data won't produce them. The feature might be genuinely hard for LLMs to do reliably, not just for a specific model or prompt.

## Concrete next steps in priority order

1. **Try Claude Sonnet on the same three commits** (requires Anthropic key). Same three configurations if you want the full sweep, or just experiment 3 (best-effort setup). If Sonnet returns ≥3/7 delete-first candidates, the honest product answer is "recommend Sonnet+ for the simplification lens; OpenAI is not supported for this lens." Costs ~$0.05 for a single-config test.
2. **If Sonnet also fails**: escalate to B's design (dedicated `lens` field + separate cap + code-side dedup + a SEPARATE LLM pass with a stripped-down simplification-only prompt). Higher scope, but the current data says prompt-only inside the main review call can't produce this.
3. **Or shelve the feature** and let the existing Suggestion Mode handle whatever deletion suggestions the LLM naturally produces (which is: nearly none, per this data).

## Experimental artifacts

Kept for reference — nothing shipped to `main`:
- `internal/prompts/main_task_system.md` — carries the value-shift + interpretation rules (still applied, not reverted)
- `cmd/sacr/prompts.go` — carries the `SACR_EVIDENCE` env-var injection hook (marked `experimental: measurement hack` in the code)
- `reports/dogfood/{3cdfc30,3426975,0fa1d85}.json` — experiment 1 raw output
- `reports/dogfood/{3cdfc30,3426975,0fa1d85}-ev.json` — experiment 2 raw output (mini + evidence)
- `reports/dogfood/{3cdfc30,3426975,0fa1d85}-4o-ev.json` — experiment 3 raw output (gpt-4o + evidence + interpretation)

**Rotate the OpenAI key now** — inline invocation on this shell will be in the history file.
