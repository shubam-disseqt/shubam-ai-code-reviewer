# docs-site — production docs for shubam-ai-code-reviewer

Public docs at [docs.sacr.dev](https://docs.sacr.dev) (post-deploy). Built with [Next.js](https://nextjs.org) + [Fumadocs](https://fumadocs.dev).

## Local dev

```bash
pnpm install
pnpm dev
# → http://localhost:3000/docs
```

## Production build

```bash
pnpm build
pnpm start        # serve the built app on :3000
```

## Type-check + lint

```bash
pnpm types:check
```

## Content layout

```
content/docs/
├── index.mdx                  # landing / at-a-glance
├── meta.json                  # top-level nav order
├── getting-started/           # install, quickstart, docker, GH action, windows
├── capabilities/              # what sacr can find — scanners, rules, overlap, SARIF, benchmarks
├── reference/                 # CLI flags, env vars, providers, provider testing
├── architecture/              # 13-stage pipeline, index+context, session log, upstream porting
└── operations/                # security, threat model, reliability, testing, troubleshooting
```

Each section carries a `meta.json` with the explicit page order.

## Adding a page

1. Drop an MDX file under the right section:
   ```
   ---
   title: My new page
   description: One-line under 140 chars.
   ---

   Body markdown here.
   ```
2. Add its slug to that section's `meta.json` in the intended position.
3. `pnpm dev` — hot reload picks it up.

MDX components auto-imported: `<Card>`, `<Cards>`, `<Callout>`, plus every native Markdown element. See `/docs/reference/*` for the full component palette Fumadocs ships.

## Deploy

- **Vercel** — connect the repo, set root directory to `docs-site/`. `vercel.json` is committed at this dir root.
- **GitHub Pages** — see `.github/workflows/docs-deploy.yml` at the repo root. Static export builds on push to `main`.
- **Self-hosted** — any Node 22+ host. `pnpm build && pnpm start` on port 3000. Reverse-proxy TLS from your load balancer.

## Source of truth

The MDX under `content/docs/` is the canonical version. The legacy HTML docs under `../docs/` are frozen — they still power `sacr docs` (embedded offline viewer) via `docs/embed.go` but new content lands here first.
