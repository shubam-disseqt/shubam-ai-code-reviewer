import Link from 'next/link';
import { Mermaid } from '@/components/mermaid';

const stats = [
  { label: 'Recall', value: '21 / 21', sub: 'seeded bugs on 10-PR audit' },
  { label: 'Precision', value: '~1 : 10', sub: 'false positives per finding' },
  { label: 'Cost', value: '$0.006', sub: 'per PR at gpt-4o-mini' },
  { label: 'Latency', value: '20–30s', sub: 'median per PR' },
];

const features = [
  {
    title: 'Inline PR review',
    body: 'Comments anchored to the exact hunk lines that broke, batched in a single review so you never trip GitHub secondary rate limits.',
  },
  {
    title: 'Reviewer-effort score',
    body: 'A deterministic 0–10 score with an audit table. Every row sums to the shown value — no vibes, no LLM guessing.',
  },
  {
    title: 'Depgraph from real imports',
    body: 'Mermaid package-import diagram parsed by go/parser and regex. Every edge maps to a literal import line.',
  },
  {
    title: 'Cross-PR overlap',
    body: 'Cheap Jaccard prefilter over open PRs, then a batched LLM verdict. Surfaces merge-conflict risk before you rebase.',
  },
  {
    title: 'Deterministic scanners',
    body: 'gitleaks, semgrep, and govulncheck run in parallel. Missing binaries log a warning and skip — never fatal.',
  },
  {
    title: 'Fingerprint carryover',
    body: 'Push a fix commit — resolved findings drop cleanly, unfixed ones carry, new bugs surface. Zero re-run spam.',
  },
  {
    title: 'SARIF output',
    body: 'Scanner findings route to GitHub Code Scanning through SARIF 2.1.0. Findings appear in the Security tab.',
  },
  {
    title: 'Two-tier model routing',
    body: 'Sonnet-class main tier for the reviewer loop, Haiku/Flash/DeepSeek cheap tier for summarizer and labeler. Saves 30–40%.',
  },
];

const principles = [
  {
    axis: 'Determinism',
    choice: 'Deterministic engineering wraps a thin LLM loop',
    why: 'Scanners, scoring, math, dedup all in Go. The LLM only makes judgement calls.',
  },
  {
    axis: 'Auditability',
    choice: 'Every claim in the PR body is verifiable',
    why: 'Effort table rows sum to the shown score. Depgraph edges equal literal import lines.',
  },
  {
    axis: 'Cost',
    choice: 'Two-tier model routing',
    why: 'Main tier for review; cheap tier for summarizer and labeler; roughly 30–40% saved.',
  },
  {
    axis: 'Idempotency',
    choice: 'Content-hash fingerprints',
    why: 'Fix commit resolves findings cleanly. No duplicate comments across pushes.',
  },
  {
    axis: 'Distribution',
    choice: 'Single Go binary',
    why: 'No Python runtime, no LangChain, no service to deploy. One binary, four output formats.',
  },
];

const integrations = [
  {
    label: 'GitHub Action',
    href: '/docs/getting-started/github-action',
    snippet: `- uses: shubam-disseqt/shubam-ai-code-reviewer@v1
  with:
    pr-number: \${{ github.event.pull_request.number }}
    api-key: \${{ secrets.OPENAI_API_KEY }}
    github-token: \${{ secrets.GITHUB_TOKEN }}`,
  },
  {
    label: 'Homebrew',
    href: '/docs/getting-started/installation',
    snippet: `brew install shubam-disseqt/tap/sacr
sacr doctor
sacr review --from main --to HEAD`,
  },
  {
    label: 'npm',
    href: '/docs/getting-started/installation',
    snippet: `npm i -g sacr
sacr review --pr 42 --format github`,
  },
  {
    label: 'Docker',
    href: '/docs/getting-started/docker',
    snippet: `docker run --rm -v "$PWD":/repo \\
  -e OPENAI_API_KEY -e GITHUB_TOKEN \\
  ghcr.io/shubam-disseqt/sacr:latest \\
  review --pr 42 --format github`,
  },
];

const pipelineDiagram = `flowchart LR
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
  end
  subgraph LLM
    C1[main tier loop]
    C2[cheap tier summary]
    C3[cheap tier labels]
  end
  subgraph Output
    D1[stdout]
    D2[json]
    D3[github PR]
    D4[sarif]
  end
  A1 --> B1 --> C1
  A2 --> B3
  A3 --> C1
  B1 --> B2 --> B3
  C1 --> B3
  B3 --> B4 --> D1
  B4 --> D2
  B4 --> D3
  B2 --> D4
  B5 --> D3
  B6 --> D3
  B7 --> D3
  C2 --> D3
  C3 --> D3`;

export default function HomePage() {
  return (
    <main className="flex flex-col">
      {/* hero */}
      <section className="relative overflow-hidden border-b border-fd-border">
        <BackgroundGrid />
        <div className="mx-auto max-w-6xl px-6 pt-20 pb-24 md:pt-28 md:pb-32">
          <p className="mb-6 inline-flex items-center gap-2 rounded-full border border-fd-border bg-fd-card/60 px-3 py-1 text-xs text-fd-muted-foreground backdrop-blur">
            <span
              aria-hidden
              className="h-1.5 w-1.5 rounded-full bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.7)]"
            />
            v1 — 100% recall on the 10-PR audit
          </p>
          <h1 className="max-w-4xl text-4xl font-semibold leading-[1.05] tracking-tight text-fd-foreground sm:text-5xl md:text-6xl lg:text-7xl">
            AI code review that
            <span className="block text-fd-muted-foreground">
              ships with your PRs.
            </span>
          </h1>
          <p className="mt-6 max-w-2xl text-lg leading-relaxed text-fd-muted-foreground">
            One Go binary. Deterministic engineering wraps a thin LLM loop —
            scanners, scoring, and dedup in Go; the model only makes judgement
            calls. Real bugs, not noise. Every claim in the PR body is
            auditable.
          </p>

          <div className="mt-10 flex flex-col gap-3 sm:flex-row">
            <Link
              href="/docs/getting-started/quickstart"
              className="inline-flex items-center justify-center gap-2 rounded-full bg-fd-foreground px-5 py-3 text-sm font-medium text-fd-background transition hover:opacity-90"
            >
              Quickstart
              <ArrowRightIcon />
            </Link>
            <Link
              href="/docs"
              className="inline-flex items-center justify-center gap-2 rounded-full border border-fd-border bg-fd-card/60 px-5 py-3 text-sm font-medium text-fd-foreground transition hover:bg-fd-card"
            >
              Read the docs
            </Link>
            <a
              href="https://github.com/shubam-disseqt/shubam-ai-code-reviewer"
              className="inline-flex items-center justify-center gap-2 rounded-full border border-fd-border bg-transparent px-5 py-3 text-sm font-medium text-fd-muted-foreground transition hover:text-fd-foreground"
            >
              <GitHubIcon />
              Star on GitHub
            </a>
          </div>

          <div className="mt-14 rounded-2xl border border-fd-border bg-fd-card/70 p-1 shadow-2xl backdrop-blur">
            <div className="flex items-center gap-2 border-b border-fd-border px-4 py-2 text-xs text-fd-muted-foreground">
              <span className="h-2.5 w-2.5 rounded-full bg-red-400/70" />
              <span className="h-2.5 w-2.5 rounded-full bg-amber-400/70" />
              <span className="h-2.5 w-2.5 rounded-full bg-emerald-400/70" />
              <span className="ml-2 select-none">~ zsh</span>
            </div>
            <pre className="overflow-x-auto px-5 py-4 text-sm leading-relaxed text-fd-foreground">
              <code>
                <span className="text-fd-muted-foreground">
                  # install, configure, and review a PR
                </span>
                {'\n'}
                <span className="text-emerald-500">$</span> brew install
                shubam-disseqt/tap/sacr{'\n'}
                <span className="text-emerald-500">$</span> export
                OPENAI_API_KEY=sk-...{'\n'}
                <span className="text-emerald-500">$</span> export
                GITHUB_TOKEN=ghp-...{'\n'}
                <span className="text-emerald-500">$</span> sacr review --pr 42
                --format github{'\n'}
                {'\n'}
                <span className="text-fd-muted-foreground">
                  ✓ 4 findings · effort 5/10 · $0.007 · 24s
                </span>
                {'\n'}
                <span className="text-fd-muted-foreground">
                  ✓ posted inline review + description block on #42
                </span>
              </code>
            </pre>
          </div>
        </div>
      </section>

      {/* stat strip */}
      <section className="border-b border-fd-border bg-fd-card/30">
        <div className="mx-auto grid max-w-6xl grid-cols-2 gap-px overflow-hidden bg-fd-border md:grid-cols-4">
          {stats.map((s) => (
            <div key={s.label} className="bg-fd-background p-6 md:p-8">
              <div className="text-xs text-fd-muted-foreground">{s.label}</div>
              <div className="mt-2 text-3xl font-semibold tracking-tight text-fd-foreground">
                {s.value}
              </div>
              <div className="mt-1 text-xs text-fd-muted-foreground">
                {s.sub}
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* what it does */}
      <section className="border-b border-fd-border">
        <div className="mx-auto max-w-6xl px-6 py-24 md:py-28">
          <SectionHeader
            eyebrow="What it does"
            title="Real outputs. Not a chatbot."
            lead="Every sacr review produces the same set of artefacts — no configuration required."
          />
          <div className="mt-12 grid gap-px overflow-hidden rounded-2xl border border-fd-border bg-fd-border md:grid-cols-2 lg:grid-cols-4">
            {features.map((f) => (
              <div
                key={f.title}
                className="group bg-fd-background p-6 transition hover:bg-fd-card/60"
              >
                <h3 className="text-sm font-semibold text-fd-foreground">
                  {f.title}
                </h3>
                <p className="mt-2 text-sm leading-relaxed text-fd-muted-foreground">
                  {f.body}
                </p>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* principles */}
      <section className="border-b border-fd-border bg-fd-card/20">
        <div className="mx-auto max-w-6xl px-6 py-24 md:py-28">
          <SectionHeader
            eyebrow="Design principles"
            title="Five choices that keep the tool honest."
            lead="Each row names a design axis, the choice sacr made, and the reason. If a claim can't be proven, it isn't in the pipeline."
          />
          <div className="mt-12 overflow-x-auto rounded-2xl border border-fd-border">
            <table className="w-full min-w-[640px] border-collapse text-left text-sm">
              <thead className="bg-fd-card/60 text-xs text-fd-muted-foreground">
                <tr>
                  <th className="px-6 py-4 font-medium">Axis</th>
                  <th className="px-6 py-4 font-medium">Choice</th>
                  <th className="px-6 py-4 font-medium">Why</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-fd-border">
                {principles.map((p) => (
                  <tr key={p.axis} className="hover:bg-fd-card/40">
                    <td className="px-6 py-5 align-top font-semibold text-fd-foreground">
                      {p.axis}
                    </td>
                    <td className="px-6 py-5 align-top text-fd-foreground">
                      {p.choice}
                    </td>
                    <td className="px-6 py-5 align-top text-fd-muted-foreground">
                      {p.why}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      {/* architecture teaser */}
      <section className="border-b border-fd-border">
        <div className="mx-auto max-w-6xl px-6 py-24 md:py-28">
          <SectionHeader
            eyebrow="Architecture"
            title="Two layers with different guarantees."
            lead="A deterministic Go pipeline with an LLM loop wedged in the middle. The model sees each file's diff and a small set of read/search tools. That is it — no agent chains, no reasoning loops, no LangGraph."
          />
          <div className="mt-10 grid gap-8 lg:grid-cols-[1.2fr_1fr] lg:items-start">
            <div className="overflow-hidden rounded-2xl border border-fd-border bg-fd-card/40 p-4">
              <Mermaid chart={pipelineDiagram} />
            </div>
            <div className="space-y-6">
              <div className="rounded-2xl border border-fd-border bg-fd-background p-6">
                <h3 className="text-sm font-semibold text-fd-foreground">
                  Deterministic path
                </h3>
                <p className="mt-2 text-sm leading-relaxed text-fd-muted-foreground">
                  Scanners, scoring, fingerprints, effort, depgraph, overlap.
                  All Go. Same input, same output every run. Reproducible,
                  auditable, cheap.
                </p>
              </div>
              <div className="rounded-2xl border border-fd-border bg-fd-background p-6">
                <h3 className="text-sm font-semibold text-fd-foreground">
                  LLM path
                </h3>
                <p className="mt-2 text-sm leading-relaxed text-fd-muted-foreground">
                  Main-tier reviewer per file plus cheap-tier summariser and
                  labeler in parallel. Judgement calls the deterministic path
                  cannot make.
                </p>
              </div>
              <Link
                href="/docs/architecture"
                className="inline-flex items-center gap-2 text-sm font-medium text-fd-foreground underline-offset-4 hover:underline"
              >
                Read the full pipeline
                <ArrowRightIcon />
              </Link>
            </div>
          </div>
        </div>
      </section>

      {/* integrations */}
      <section className="border-b border-fd-border bg-fd-card/20">
        <div className="mx-auto max-w-6xl px-6 py-24 md:py-28">
          <SectionHeader
            eyebrow="Integrate"
            title="Ship in three lines."
            lead="One binary, five distribution channels. Pick the one that already lives in your pipeline."
          />
          <div className="mt-12 grid gap-6 md:grid-cols-2">
            {integrations.map((i) => (
              <div
                key={i.label}
                className="overflow-hidden rounded-2xl border border-fd-border bg-fd-background"
              >
                <div className="flex items-center justify-between border-b border-fd-border px-5 py-3">
                  <span className="text-sm font-semibold text-fd-foreground">
                    {i.label}
                  </span>
                  <Link
                    href={i.href}
                    className="text-xs text-fd-muted-foreground hover:text-fd-foreground"
                  >
                    Setup →
                  </Link>
                </div>
                <pre className="overflow-x-auto px-5 py-4 text-xs leading-relaxed text-fd-foreground">
                  <code>{i.snippet}</code>
                </pre>
              </div>
            ))}
          </div>
          <p className="mt-8 text-sm text-fd-muted-foreground">
            Also available: direct binary download, VS Code extension skeleton,
            and{' '}
            <Link
              href="/docs/getting-started/windows"
              className="text-fd-foreground underline-offset-4 hover:underline"
            >
              Windows setup
            </Link>
            .
          </p>
        </div>
      </section>

      {/* final CTA */}
      <section className="relative overflow-hidden">
        <BackgroundGrid />
        <div className="mx-auto max-w-4xl px-6 py-24 text-center md:py-32">
          <h2 className="text-3xl font-semibold tracking-tight text-fd-foreground sm:text-4xl md:text-5xl">
            Bring your own model.
            <span className="block text-fd-muted-foreground">
              Own the data. Verify every claim.
            </span>
          </h2>
          <p className="mx-auto mt-6 max-w-xl text-base leading-relaxed text-fd-muted-foreground">
            sacr is Apache-2.0, self-hosted, and provider-agnostic. Point it at
            OpenAI, Anthropic, AWS Bedrock, or DeepSeek — the pipeline is the
            same.
          </p>
          <div className="mt-10 flex flex-col justify-center gap-3 sm:flex-row">
            <Link
              href="/docs/getting-started/quickstart"
              className="inline-flex items-center justify-center gap-2 rounded-full bg-fd-foreground px-6 py-3 text-sm font-medium text-fd-background transition hover:opacity-90"
            >
              Get started
              <ArrowRightIcon />
            </Link>
            <a
              href="https://github.com/shubam-disseqt/shubam-ai-code-reviewer"
              className="inline-flex items-center justify-center gap-2 rounded-full border border-fd-border px-6 py-3 text-sm font-medium text-fd-foreground transition hover:bg-fd-card"
            >
              <GitHubIcon />
              View source
            </a>
          </div>
        </div>
      </section>
    </main>
  );
}

function SectionHeader({
  eyebrow,
  title,
  lead,
}: {
  eyebrow: string;
  title: string;
  lead: string;
}) {
  return (
    <div className="max-w-3xl">
      <div className="text-xs font-medium text-emerald-600 dark:text-emerald-400">
        {eyebrow}
      </div>
      <h2 className="mt-3 text-3xl font-semibold tracking-tight text-fd-foreground sm:text-4xl md:text-[2.75rem] md:leading-[1.1]">
        {title}
      </h2>
      <p className="mt-5 text-base leading-relaxed text-fd-muted-foreground md:text-lg">
        {lead}
      </p>
    </div>
  );
}

function BackgroundGrid() {
  return (
    <div
      aria-hidden
      className="pointer-events-none absolute inset-0 -z-10 opacity-60 [background-image:linear-gradient(to_right,rgba(120,120,120,0.08)_1px,transparent_1px),linear-gradient(to_bottom,rgba(120,120,120,0.08)_1px,transparent_1px)] [background-size:44px_44px]"
    />
  );
}

function ArrowRightIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="h-4 w-4"
    >
      <path d="M5 12h14" />
      <path d="m12 5 7 7-7 7" />
    </svg>
  );
}

function GitHubIcon() {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="currentColor"
      className="h-4 w-4"
    >
      <path d="M12 .5A11.5 11.5 0 0 0 .5 12a11.5 11.5 0 0 0 7.86 10.92c.57.1.78-.25.78-.55v-2c-3.2.7-3.87-1.37-3.87-1.37-.53-1.32-1.29-1.67-1.29-1.67-1.05-.71.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.55-.29-5.24-1.28-5.24-5.7 0-1.26.45-2.29 1.19-3.1-.12-.29-.52-1.47.11-3.05 0 0 .97-.31 3.18 1.18a11 11 0 0 1 5.79 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.58.23 2.76.11 3.05.74.81 1.18 1.84 1.18 3.1 0 4.43-2.69 5.4-5.26 5.68.41.36.78 1.06.78 2.15v3.18c0 .31.21.66.79.55A11.5 11.5 0 0 0 23.5 12 11.5 11.5 0 0 0 12 .5Z" />
    </svg>
  );
}
