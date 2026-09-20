# sacr vs OCR — issue-finding capability audit

**Answer up top:** sacr and OCR are **not equal**. sacr catches a broader set of bug classes than OCR (scanners, org rules, cross-file context, cross-PR overlap), but has **one real regression in comment reliability**: it dropped OCR's LLM-based comment relocation, so a subset of LLM-found issues that OCR would have posted are now silently discarded before hitting GitHub.

Every claim below cites source code, not READMEs.

---

## 1. What is IDENTICAL — no regression

| Component | Path (both sides identical) | LOC |
|---|---|---|
| Unified-diff parser | `internal/diff/parser.go` | 150 |
| Hunk parser | `internal/diff/hunk.go` | 113 |
| Line-snapping resolver | `internal/diff/resolver.go` | 306 |
| JSON comment-args repair | OCR `internal/tool/comment_args_repair.go` → sacr `internal/comment/args_repair.go` | 291 |
| Resolver regression harness | `internal/diff/resolver_test.go` | 880 |
| Tool registry (6 tools) | `internal/tool/definitions.go:17-25` | — |
| `code_search`, `file_read`, `file_read_diff`, `file_find`, `filereader` | `internal/tool/*.go` | byte-equivalent |
| Empty-round stop (3 consecutive) | `internal/llmloop/loop.go` | matched |
| Grace round (last-round = `code_comment` + `task_done` only) | `internal/llmloop/loop.go` | matched |
| Memory compression | `internal/llmloop/compression.go` | ~identical |
| Severity taxonomy | `critical / high / medium / low` | matched |
| Category taxonomy | `bug, security, performance, maintainability, test, style, documentation, other` | matched |

**What this means:** the *core LLM review loop and its accuracy machinery are the same code on both sides.* Whatever OCR's LLM can find in a hunk, sacr's LLM sees the same tools, the same round budget, the same repair for malformed JSON, and the same line-snapping algorithm.

---

## 2. What sacr REGRESSED — concrete losses

### 2.1. LLM comment relocation was gutted (the one real bug-catching regression)

- **OCR** `refs/ocr/internal/diff/relocation.go` — 117 LOC, exports `BuildReLocationMessages` (line 28) + `ReLocateComment` (line 49). Loop calls it after `RelocateAcrossFiles` fails (`refs/ocr/internal/llmloop/loop.go:689-724`).
- **sacr** `shubam-ai-code-reviewer/internal/diff/relocation.go` — 30 LOC. `ReLocateComment` and `BuildReLocationMessages` **do not exist** (verified: `grep -n "ReLocateComment\|BuildReLocationMessages"` returns 0 matches). Only `extractCodeBlock` + `RelocateAcrossFiles` survived.
- Loop's own comment marks it: `shubam-ai-code-reviewer/internal/llmloop/loop.go:584-586` — *"note: dropped the LLM re-location step, upgrade path is to port internal/diff.ReLocateComment + prompts.ReLocationTask."*
- Tests dropped: `refs/ocr/internal/diff/relocation_test.go` = 297 LOC vs `shubam-ai-code-reviewer/internal/diff/relocation_test.go` = 95 LOC. Seven test cases removed (`TestReLocateComment_*`, `TestBuildReLocationMessages_*`).

**How this becomes a silent bug-catching loss:**
1. LLM emits a `code_comment` with `existing_code` that has drifted (whitespace normalized differently, refactor moved the anchor, LLM misfiled which file the code lives in).
2. `ResolveComment` (`internal/diff/resolver.go:60`) fails to snap; `RelocateAcrossFiles` also fails (multiple candidates or none).
3. Comment reaches `filterResolved` in `cmd/sacr/review_cmd.go:619-627` with `StartLine==0 && EndLine==0`.
4. Comment is dropped. Second drop at `cmd/sacr/emit.go:282-285` catches any that slipped past.
5. Reported as a "skipped N unresolved" count in the summary — never appears on the PR.

OCR would have re-prompted the LLM with the diff to rewrite `existing_code`, then re-attempted the snap. In practice this recovers most drifted anchors.

**Blast-radius estimate is not derivable from code alone.** But every LLM comment that fails primary + cross-file positioning today falls through to /dev/null instead of a rescue.

### 2.2. Findings filter can silently suppress LLM findings

New in sacr only: `cmd/sacr/review_cmd.go:320` — `filterByScore(comments, policy, minSev)` applies `--min-severity` (default MEDIUM) and drops anything the scoring policy marks `SUPPRESS`. OCR emits everything the LLM produces.

- Default value: if you don't pass `--min-severity`, LOW-severity findings from the LLM never reach output.
- Scoring policy override lives in `internal/scoring/scoring.go:43-70`; a repo-local `.sacr/scoring.yaml` can silence whole categories.

**This is a policy knob, not a bug.** But by default, sacr surfaces fewer low-severity issues than OCR would emit for the same PR. It's a regression on *raw recall*, an improvement on *signal-to-noise*.

### 2.3. Observability regressions (do NOT affect bug-catching, listed for completeness)

- Telemetry spans stripped from `internal/llmloop/loop.go` (Agent 1 report).
- `NewRequestMeta` / per-request identity dropped.
- Task-type enum reduced from 6 (main/compress/plan/grouping/relocation/filter) → 2 (main/compress) in `internal/llmloop/session.go:24-28`.

None of these change what the LLM can find; they only change how easily you debug a slow or wrong review.

---

## 3. What sacr EXPANDED beyond OCR — new bug-finding surface

### 3.1. Deterministic scanners (three new detection classes)

None of these exist in OCR. Verified: `refs/ocr/internal/scanner/` does not exist; `refs/ocr/internal/scan/` handles scan templates, not bug scanning.

| Scanner | sacr path | What it catches |
|---|---|---|
| gitleaks | `internal/scanner/gitleaks.go:37-96` | Hardcoded secrets, API keys, tokens |
| semgrep | `internal/scanner/semgrep.go:66-95` | SQL injection, XSS, unsafe crypto, pattern-based bugs |
| govulncheck | `internal/scanner/govulncheck.go:61-96` | Known-vulnerable Go dependencies (CVE) |

Wiring: `cmd/sacr/review_cmd.go:190-196` (Phase 3.5 runs scanners) → `cmd/sacr/scanners.go:132-153` (converts findings to `model.LlmComment` with `Source="scanner:<tool>"`) → merged into the same `CommentCollector` the LLM writes to (`cmd/sacr/review_cmd.go:265-266`).

Scanner findings are also rendered into the LLM's system prompt as "Known Issues" (`internal/prompts/main_task_system.md:29-30`) so the LLM doesn't re-report them and can add context.

**Net effect:** three whole detection categories with high-recall deterministic tools that OCR cannot catch at all.

### 3.2. Suggestion Mode — added maintainability findings

Prompt expansion:
- OCR `refs/ocr/internal/config/template/prompts/main_task_system.md` — 25 lines, **0 mentions of "suggestion"** (grep confirmed).
- sacr `internal/prompts/main_task_system.md` — 126 lines, **19 mentions of "suggestion"**. Lines 32-126 define a whole Suggestion Mode with categories `extract-helper / naming / idiomatic-go / error-wrapping / guard-clause / missing-test / missing-docs`.

OCR does not instruct the LLM to look for maintainability nudges. sacr does.

### 3.3. Dynamic org rules

- OCR: `refs/ocr/internal/config/rules/system_rules.go` — embedded, static, requires rebuild to change.
- sacr: `internal/rules/` — loads from `SACR_ORG_RULES_REPO` at review time (`cmd/sacr/review_cmd.go:449-469`), injects up to 15 rules into the prompt as a `## Custom Review Rules` block (`internal/rules/inject.go:30-51`).

Both sides can enforce business rules; sacr can update them without a release.

### 3.4. Cross-file context (JIT / SQLite index)

- OCR: no `index`, no `reviewctx`, no `chunker` directories. LLM sees only the diff.
- sacr: `internal/index/`, `internal/chunker/`, `internal/reviewctx/`. `cmd/sacr/review_cmd.go:220-224` builds an indexed-first / JIT-fallback context block that goes into the system prompt (`cmd/sacr/prompts.go:47-50`).

When the index is populated (subsequent reviews of the same repo), the LLM can reason about blast radius and cross-file impact. OCR cannot see beyond the diff.

### 3.5. Cross-PR overlap detection

Zero equivalent in OCR. `internal/overlap/overlap.go:26-100` — deterministic file-overlap prefilter → LLM verdict on top-N candidates → confidence-filtered findings. Wired at `cmd/sacr/review_cmd.go:333-334`.

Not really a bug detector — an operational one. But if two PRs modify overlapping code, sacr flags it; OCR would silently miss the conflict.

---

## 4. Bug-class × side matrix (code-anchored)

| Bug class | OCR catches | sacr catches | Evidence |
|---|---|---|---|
| Hardcoded secrets | LLM only (low recall) | Deterministic + LLM | `internal/scanner/gitleaks.go:37-96` |
| Known vulnerable deps (CVE) | LLM only (low recall) | Deterministic + LLM | `internal/scanner/govulncheck.go:61-96` |
| Pattern-based bugs (SQLi/XSS/unsafe crypto) | LLM only | Deterministic + LLM | `internal/scanner/semgrep.go:66-95` |
| Business/org rule violations | Static system rules | Dynamic org rules (up to 15) | `internal/rules/inject.go:16,30-51` |
| Cross-file / blast-radius bugs | LLM w/ diff only | LLM w/ cross-file context | `internal/reviewctx/build.go:17-30` |
| Cross-PR overlap / conflict risk | Not detected | Detected | `internal/overlap/overlap.go:26-100` |
| LLM-judged logic bugs (off-by-one, races, misuse) | LLM loop | Same LLM loop | `internal/llmloop/loop.go` (identical algorithm) |
| Maintainability nudges (extract-helper, guard clauses, missing tests/docs) | Not instructed | Instructed via Suggestion Mode | `internal/prompts/main_task_system.md:32-126` |
| Comments on drifted anchors (whitespace normalized differently, code moved) | Recovered via LLM re-locate | **Silently dropped** | `internal/diff/relocation.go` (30 LOC, no `ReLocateComment`); `cmd/sacr/review_cmd.go:619-627` |
| LOW-severity LLM findings by default | Emitted | Filtered by `--min-severity` (default MEDIUM) | `cmd/sacr/review_cmd.go:320`; `internal/scoring/scoring.go:43-70` |

---

## 5. Verdict

**On the LLM's ability to reason about a hunk and produce a finding:** identical. Same tools, same loop, same repair, same prompt for the actual "look for these bugs" instructions. Plus a whole added Suggestion Mode block.

**On the deterministic layer around the LLM:** sacr is strictly a superset — three scanners, org rules, cross-file context, cross-PR overlap. OCR has none of this.

**On the delivery of found issues to the reviewer:** sacr has **one real regression** — dropped LLM re-locate means comments whose `existing_code` anchor doesn't match verbatim get silently discarded by `filterResolved` instead of rescued. This is a deliberate simplification (see the `note:` comment in loop.go:584) and documented, but bugs that OCR would have surfaced now never post to the PR.

**On volume of output by default:** sacr will show fewer LOW-severity issues than OCR because `--min-severity` defaults to MEDIUM. Configurable, not fatal.

### Are they "equally able to get the issues"?

- **In terms of what CAN be found:** sacr > OCR. Three new deterministic detection classes plus context tools give sacr a larger detection surface.
- **In terms of what actually reaches the PR:** sacr MAY show fewer LLM findings than OCR in two specific cases:
  1. LLM comment whose anchor drifted → dropped by `filterResolved`, would have been recovered by OCR's re-locate.
  2. LOW-severity LLM findings → filtered out by default min-severity.
- **In terms of the same LLM finding the same bug:** identical.

**Recommendation, ranked by ROI:**

1. **Port `ReLocateComment` back** (biggest concrete recall loss). `refs/ocr/internal/diff/relocation.go:44-95` is 52 LOC of pure logic; only rewire its LLM client call to sacr's `internal/llm`. The 7 dropped tests in `relocation_test.go` are the regression harness for this port.
2. **Add a `dropped_unresolved` visible counter to the PR body**, not just stdout. Right now silent drops are invisible to reviewers.
3. **Document that `--min-severity` defaults to MEDIUM** — surprising if you're migrating from OCR where all LLM comments were emitted.
4. Nothing to do about scanners / org rules / overlap / reviewctx — those are pure wins.
