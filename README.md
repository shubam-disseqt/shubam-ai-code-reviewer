<div align="center">
  
# sacr

### AI code review that ships with your PRs

<sub>Deterministic engineering · thin agent loop · one Go binary · real bugs, not noise</sub>

<br>

[![CI](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/actions/workflows/ci.yml/badge.svg)](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/actions/workflows/ci.yml)
[![govulncheck](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/actions/workflows/govulncheck.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square)](LICENSE)
[![Go 1.26+](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white&style=flat-square)](go.mod)
[![SLSA](https://img.shields.io/badge/SLSA-build--provenance-D4AF37?style=flat-square)](https://slsa.dev)

**[Website](https://shubam-disseqt.github.io/shubam-ai-code-reviewer/)** · **[Docs](https://shubam-disseqt.github.io/shubam-ai-code-reviewer/docs)** · [Quickstart](#quick-start) · [Architecture](#architecture) · [Integrate](#integrate)

<br>

</div>

---

## Quick start

```bash
brew install shubam-disseqt/tap/sacr
export OPENAI_API_KEY=sk-...
sacr review --pr 42 --format github
```

Every PR gets inline review, effort score, package-imports diagram, and risk labels. Push a fix commit — resolved findings drop, unfixed carry, new bugs surface. Full flags: [CLI reference](https://shubam-disseqt.github.io/shubam-ai-code-reviewer/docs/reference/cli).

---

## Features

<table>
<tr>
<th align="left" width="34%">On the PR</th>
<th align="left" width="33%">Analysis</th>
<th align="left" width="33%">State across runs</th>
</tr>
<tr>
<td valign="top">

- **Inline comments** on exact lines
- **Summary comment** as bot
- **Committable suggestions**
- **Structured labels**

</td>
<td valign="top">

- **Effort score** 0–10
- **Package-imports** diagram
- **Deterministic scanners**<br/><sub>gitleaks · semgrep · govulncheck</sub>
- **SARIF 2.1.0** output
- **Two-tier LLM** routing
- **Cross-PR overlap** detection

</td>
<td valign="top">

- **Fingerprint carryover**
- **Always-on code index** (per-repo SQLite)
- **Org-level rules** (YAML)
- **Session log** (JSONL)

</td>
</tr>
</table>

Per-feature detail: [capabilities docs](https://shubam-disseqt.github.io/shubam-ai-code-reviewer/docs/capabilities).

---

## Architecture

```mermaid
flowchart LR
  subgraph Offline["Index (always on)"]
    IX0["sacr index"] --> IX1[(SQLite)]
  end

  subgraph Input
    A1[git diff]
    A2[.sacr policy]
    A3[env keys]
  end

  subgraph Deterministic
    B1[selector]
    B2[scanners]
    B3[scoring]
    B4[fingerprint]
    B5[effort]
    B6[depgraph]
    B7[overlap]
    CTX["reviewctx<br/>indexed OR JIT"]
  end

  subgraph LLM
    C1[main tier<br/>per-file review]
    C2[cheap tier<br/>summariser]
    C3[cheap tier<br/>labeler]
  end

  subgraph Output
    D1[stdout · json]
    D3[github]
    D4[sarif]
  end

  A1 --> B1 --> CTX --> C1
  IX1 -. read at review time .-> CTX
  A2 --> B3
  A3 --> C1
  B1 --> B2
  C1 --> B3 --> B4 --> D1
  B4 --> D3
  B2 --> D4
  B5 --> D3
  B6 --> D3
  B7 --> D3
  C2 --> D3
  C3 --> D3
```

Two layers, different guarantees. The **deterministic path** is pure Go: same input → same output. The **LLM path** handles only the judgement calls the deterministic side cannot make.

<details>
<summary><strong>13-stage pipeline</strong></summary>

```mermaid
flowchart TD
    P0["0 · scoring"]
    P1["1 · diff"]
    P2["2 · selector"]
    P3["3 · index"]
    P32{{"3.2 · scanners"}}
    P33{{"3.3 · summary + labels"}}
    P4["4 · rules"]
    P5["5 · context"]
    P10["10 · LLM loop"]
    P11["11 · post-process"]
    P115["11.5 · carryover"]
    P12{{"12 · overlap"}}
    P125{{"12.5 · effort"}}
    P126{{"12.6 · depgraph"}}
    P13["13 · emit"]

    P0 --> P1 --> P2 --> P3
    P3 -.-> P32
    P3 -.-> P33
    P3 --> P4 --> P5 --> P10 --> P11 --> P115 --> P13
    P115 -.-> P12
    P115 -.-> P125
    P115 -.-> P126
    P32 -.-> P13
    P33 -.-> P13
    P12 -.-> P13
    P125 -.-> P13
    P126 -.-> P13
```

**Fatal** (abort with wrapped error): diff, scoring, index store, session, LLM, prompts, tools, emit.
**Fault-tolerant** (warn + continue): scanners, cheap-tier agents, index summaries, overlap, effort, depgraph.

</details>

Full package map: [architecture docs](https://shubam-disseqt.github.io/shubam-ai-code-reviewer/docs/architecture/pipeline).

---

## Integrate

```yaml
- uses: shubam-disseqt/shubam-ai-code-reviewer@v1
  with:
    pr-number: ${{ github.event.pull_request.number }}
    api-key: ${{ secrets.OPENAI_API_KEY }}
    github-token: ${{ secrets.GITHUB_TOKEN }}
```

Homebrew · npm · Docker · direct binary: [installation docs](https://shubam-disseqt.github.io/shubam-ai-code-reviewer/docs/getting-started/installation).

---

## Why sacr

|  | sacr | CodeRabbit | Greptile | Ellipsis |
|---|:-:|:-:|:-:|:-:|
| Single binary | Yes | — | — | — |
| Deterministic scoring | Yes | — | — | — |
| Depgraph from real imports | Yes | — | — | — |
| Local-only mode | Yes | — | — | — |
| Choice of LLM provider | 4 | 1 | 1 | 1 |
| Cost per PR | **~$0.006** | $8–15 flat | $12/user/mo | $20/user/mo |

Bring your own model. Own the data. Verify every claim.

---

<div align="center">

**Apache-2.0**
[Website](https://shubam-disseqt.github.io/shubam-ai-code-reviewer/) · [Docs](https://shubam-disseqt.github.io/shubam-ai-code-reviewer/docs) · [Issues](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/issues) · [Discussions](https://github.com/shubam-disseqt/shubam-ai-code-reviewer/discussions) · [Roadmap](ROADMAP.md)

</div>
